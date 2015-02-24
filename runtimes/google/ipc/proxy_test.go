package ipc_test

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vtrace"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/modules/core"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	iipc "v.io/core/veyron/runtimes/google/ipc"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	inaming "v.io/core/veyron/runtimes/google/naming"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

func testContext() *context.T {
	ctx, _ := context.WithTimeout(testContextWithoutDeadline(), 20*time.Second)
	return ctx
}

func testContextWithoutDeadline() *context.T {
	ctx, _ := context.RootContext()
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{})
	if err != nil {
		panic(err)
	}
	ctx, _ = vtrace.SetNewTrace(ctx)
	return ctx
}

type testServer struct{}

func (*testServer) Echo(ctx ipc.ServerContext, arg string) (string, error) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", ctx.Method(), ctx.Suffix(), arg), nil
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(c security.Context) error {
	return nil
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return t.server, testServerAuthorizer{}, nil
}

type proxyHandle struct {
	ns    naming.Namespace
	sh    *modules.Shell
	proxy modules.Handle
	name  string
}

func (h *proxyHandle) Start(t *testing.T, ctx *context.T, args ...string) error {
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.sh = sh
	p, err := sh.Start(core.ProxyServerCommand, nil, args...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.proxy = p
	s := expect.NewSession(t, p.Stdout(), time.Minute)
	s.ReadLine()
	h.name = s.ExpectVar("PROXY_NAME")
	if len(h.name) == 0 {
		t.Fatalf("failed to get PROXY_NAME from proxyd")
	}
	return h.ns.Mount(ctx, "proxy", h.name, time.Hour)
}

func (h *proxyHandle) Stop(ctx *context.T) error {
	defer h.sh.Cleanup(os.Stderr, os.Stderr)
	if err := h.proxy.Shutdown(os.Stderr, os.Stderr); err != nil {
		return err
	}
	if len(h.name) == 0 {
		return nil
	}
	return h.ns.Unmount(ctx, "proxy", h.name)
}

func TestProxyOnly(t *testing.T) {
	listenSpec := ipc.ListenSpec{Proxy: "proxy"}
	testProxy(t, listenSpec)
}

func TestProxy(t *testing.T) {
	proxyListenSpec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	proxyListenSpec.Proxy = "proxy"
	testProxy(t, proxyListenSpec)
}

func TestWSProxy(t *testing.T) {
	proxyListenSpec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	proxyListenSpec.Proxy = "proxy"
	// The proxy uses websockets only, but the server is using tcp.
	testProxy(t, proxyListenSpec, "--veyron.tcp.protocol=ws")
}

func testProxy(t *testing.T, spec ipc.ListenSpec, args ...string) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	client, err := iipc.InternalNewClient(sm, ns, vc.LocalPrincipal{tsecurity.NewPrincipal("client")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := testContext()
	server, err := iipc.InternalNewServer(ctx, sm, ns, nil, vc.LocalPrincipal{tsecurity.NewPrincipal("server")})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// If no address is specified then we'll only 'listen' via
	// the proxy.
	hasLocalListener := len(spec.Addrs) > 0 && len(spec.Addrs[0].Address) != 0

	name := "mountpoint/server/suffix"
	makeCall := func(opts ...ipc.CallOpt) (string, error) {
		ctx, _ := context.WithDeadline(testContext(), time.Now().Add(5*time.Second))
		// Let's fail fast so that the tests don't take as long to run.
		call, err := client.StartCall(ctx, name, "Echo", []interface{}{"batman"}, opts...)
		if err != nil {
			// proxy is down, we should return here/.... prepend
			// the error with a well known string so that we can test for that.
			return "", fmt.Errorf("RESOLVE: %s", err)
		}
		var result string
		if err = call.Finish(&result); err != nil {
			return "", err
		}
		return result, nil
	}
	proxy := &proxyHandle{ns: ns}
	if err := proxy.Start(t, ctx, args...); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop(ctx)
	addrs := verifyMount(t, ctx, ns, spec.Proxy)
	if len(addrs) != 1 {
		t.Fatalf("failed to lookup proxy")
	}

	eps, err := server.Listen(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.ServeDispatcher("mountpoint/server", testServerDisp{&testServer{}}); err != nil {
		t.Fatal(err)
	}

	// Proxy connections are started asynchronously, so we need to wait..
	waitForMountTable := func(ch chan int, expect int) {
		then := time.Now().Add(time.Minute)
		for {
			me, err := ns.Resolve(ctx, name)
			if err == nil && len(me.Servers) == expect {
				ch <- 1
				return
			}
			if time.Now().After(then) {
				t.Fatalf("timed out")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	waitForServerStatus := func(ch chan int, proxy string) {
		then := time.Now().Add(time.Minute)
		for {
			status := server.Status()
			if len(status.Proxies) == 1 && status.Proxies[0].Proxy == proxy {
				ch <- 2
				return
			}
			if time.Now().After(then) {
				t.Fatalf("timed out")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	proxyEP, _ := naming.SplitAddressName(addrs[0])
	proxiedEP, err := inaming.NewEndpoint(proxyEP)
	if err != nil {
		t.Fatalf("unexpected error for %q: %s", proxyEP, err)
	}
	proxiedEP.RID = naming.FixedRoutingID(0x555555555)
	expectedNames := []string{naming.JoinAddressName(proxiedEP.String(), "suffix")}
	if hasLocalListener {
		expectedNames = append(expectedNames, naming.JoinAddressName(eps[0].String(), "suffix"))
	}

	// Proxy connetions are created asynchronously, so we wait for the
	// expected number of endpoints to appear for the specified service name.
	ch := make(chan int, 2)
	go waitForMountTable(ch, len(expectedNames))
	go waitForServerStatus(ch, spec.Proxy)
	select {
	case <-time.After(time.Minute):
		t.Fatalf("timedout waiting for two entries in the mount table and server status")
	case i := <-ch:
		select {
		case <-time.After(time.Minute):
			t.Fatalf("timedout waiting for two entries in the mount table or server status")
		case j := <-ch:
			if !((i == 1 && j == 2) || (i == 2 && j == 1)) {
				t.Fatalf("unexpected return values from waiters")
			}
		}
	}

	status := server.Status()
	if got, want := status.Proxies[0].Endpoint, proxiedEP; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	got := []string{}
	for _, s := range verifyMount(t, ctx, ns, name) {
		got = append(got, s)
	}
	sort.Strings(got)
	sort.Strings(expectedNames)
	if !reflect.DeepEqual(got, expectedNames) {
		t.Errorf("got %v, want %v", got, expectedNames)
	}

	if hasLocalListener {
		// Listen will publish both the local and proxied endpoint with the
		// mount table, given that we're trying to test the proxy, we remove
		// the local endpoint from the mount table entry!  We have to remove both
		// the tcp and the websocket address.
		sep := eps[0].String()
		ns.Unmount(ctx, "mountpoint/server", sep)
	}

	addrs = verifyMount(t, ctx, ns, name)
	if len(addrs) != 1 {
		t.Fatalf("failed to lookup proxy: addrs %v", addrs)
	}

	// Proxied endpoint should be published and RPC should succeed (through proxy)
	const expected = `method:"Echo",suffix:"suffix",arg:"batman"`
	if result, err := makeCall(); result != expected || err != nil {
		t.Fatalf("Got (%v, %v) want (%v, nil)", result, err, expected)
	}
	// Proxy dies, calls should fail and the name should be unmounted.
	if err := proxy.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	if result, err := makeCall(options.NoRetry{}); err == nil || (!strings.HasPrefix(err.Error(), "RESOLVE") && !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf(`Got (%v, %v) want ("", "RESOLVE: <err>" or "EOF") as proxy is down`, result, err)
	}

	for {
		if _, err := ns.Resolve(ctx, name); err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	verifyMountMissing(t, ctx, ns, name)

	status = server.Status()
	if len(status.Proxies) != 1 || status.Proxies[0].Proxy != spec.Proxy || !verror.Is(status.Proxies[0].Error, verror.ErrNoServers.ID) {
		t.Fatalf("proxy status is incorrect: %v", status.Proxies)
	}

	// Proxy restarts, calls should eventually start succeeding.
	if err := proxy.Start(t, ctx, args...); err != nil {
		t.Fatal(err)
	}

	retries := 0
	for {
		if result, err := makeCall(); err == nil {
			if result != expected {
				t.Errorf("Got (%v, %v) want (%v, nil)", result, err, expected)
			}
			break
		} else {
			retries++
			if retries > 10 {
				t.Fatalf("Failed after 10 attempts: err: %s", err)
			}
		}
	}
}

func verifyMount(t *testing.T, ctx *context.T, ns naming.Namespace, name string) []string {
	me, err := ns.Resolve(ctx, name)
	if err != nil {
		t.Errorf("%s not found in mounttable", name)
		return nil
	}
	return me.Names()
}

func verifyMountMissing(t *testing.T, ctx *context.T, ns naming.Namespace, name string) {
	if me, err := ns.Resolve(ctx, name); err == nil {
		names := me.Names()
		t.Errorf("%s not supposed to be found in mounttable; got %d servers instead: %v", name, len(names), names)
	}
}
