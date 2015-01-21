package ipc

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/modules/core"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	inaming "v.io/core/veyron/runtimes/google/naming"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
)

type noMethodsType struct{ Field string }

type fieldType struct {
	unexported string
}
type noExportedFieldsType struct{}

func (noExportedFieldsType) F(_ ipc.ServerContext, f fieldType) error { return nil }

type badObjectDispatcher struct{}

func (badObjectDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return noMethodsType{}, nil, nil
}

// TestBadObject ensures that Serve handles bad reciver objects gracefully (in
// particular, it doesn't panic).
func TestBadObject(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()

	server, err := InternalNewServer(testContext(), sm, ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	if _, err := server.Listen(listenSpec); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	if err := server.Serve("", nil, nil); err == nil {
		t.Fatal("should have failed")
	}
	if err := server.Serve("", new(noMethodsType), nil); err == nil {
		t.Fatal("should have failed")
	}
	if err := server.Serve("", new(noExportedFieldsType), nil); err == nil {
		t.Fatal("should have failed")
	}
	if err := server.ServeDispatcher("servername", badObjectDispatcher{}); err != nil {
		t.Fatalf("ServeDispatcher failed: %v", err)
	}
	client, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	ctx, _ := context.WithDeadline(testContext(), time.Now().Add(10*time.Second))
	call, err := client.StartCall(ctx, "servername", "SomeMethod", nil)
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}
	var result string
	var rerr error
	if err = call.Finish(&result, &rerr); err == nil {
		// TODO(caprita): Check the error type rather than
		// merely ensuring the test doesn't panic.
		t.Fatalf("should have failed")
	}
}

type proxyHandle struct {
	ns    naming.Namespace
	sh    *modules.Shell
	proxy modules.Handle
	name  string
}

func (h *proxyHandle) Start(t *testing.T, args ...string) error {
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.sh = sh
	p, err := sh.Start(core.ProxyServerCommand, nil, args...)
	if err != nil {
		p.Shutdown(os.Stderr, os.Stderr)
		t.Fatalf("unexpected error: %s", err)
	}
	h.proxy = p
	s := expect.NewSession(t, p.Stdout(), time.Minute)
	s.ReadLine()
	h.name = s.ExpectVar("PROXY_NAME")
	if len(h.name) == 0 {
		t.Fatalf("failed to get PROXY_NAME from proxyd")
	}
	return h.ns.Mount(testContext(), "proxy", h.name, time.Hour)
}

func (h *proxyHandle) Stop() error {
	defer h.sh.Cleanup(os.Stderr, os.Stderr)
	if err := h.proxy.Shutdown(os.Stderr, os.Stderr); err != nil {
		return err
	}
	if len(h.name) == 0 {
		return nil
	}
	return h.ns.Unmount(testContext(), "proxy", h.name)
}

func TestProxyOnly(t *testing.T) {
	listenSpec := ipc.ListenSpec{Proxy: "proxy"}
	testProxy(t, listenSpec, "--", "--veyron.tcp.address=127.0.0.1:0")
}

func TestProxy(t *testing.T) {
	proxyListenSpec := listenSpec
	proxyListenSpec.Proxy = "proxy"
	testProxy(t, proxyListenSpec, "--", "--veyron.tcp.address=127.0.0.1:0")
}

func TestWSProxy(t *testing.T) {
	proxyListenSpec := listenSpec
	proxyListenSpec.Proxy = "proxy"
	// The proxy uses websockets only, but the server is using tcp.
	testProxy(t, proxyListenSpec, "--", "--veyron.tcp.protocol=ws", "--veyron.tcp.address=127.0.0.1:0")
}

func testProxy(t *testing.T, spec ipc.ListenSpec, args ...string) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	client, err := InternalNewClient(sm, ns, vc.LocalPrincipal{tsecurity.NewPrincipal("client")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{tsecurity.NewPrincipal("server")})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// If no address is specified then we'll only 'listen' via
	// the proxy.
	hasLocalListener := len(spec.Addrs) > 0 && len(spec.Addrs[0].Address) != 0

	name := "mountpoint/server/suffix"
	makeCall := func() (string, error) {
		ctx, _ := context.WithDeadline(testContext(), time.Now().Add(5*time.Second))
		// Let's fail fast so that the tests don't take as long to run.
		call, err := client.StartCall(ctx, name, "Echo", []interface{}{"batman"})
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
	if err := proxy.Start(t, args...); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop()
	addrs := verifyMount(t, ns, "proxy")
	if len(addrs) != 1 {
		t.Fatalf("failed to lookup proxy")
	}
	proxyEP, _ := naming.SplitAddressName(addrs[0])

	eps, err := server.Listen(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.ServeDispatcher("mountpoint/server", testServerDisp{&testServer{}}); err != nil {
		t.Fatal(err)
	}

	ch := make(chan struct{})
	// Proxy connections are started asynchronously, so we need to wait..
	waitfor := func(expect int) {
		then := time.Now().Add(time.Minute)
		for {
			me, err := ns.Resolve(testContext(), name)
			if err == nil && len(me.Servers) == expect {
				close(ch)
				return
			}
			if time.Now().After(then) {
				t.Fatalf("timed out")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

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
	go waitfor(len(expectedNames))
	select {
	case <-time.After(time.Minute):
		t.Fatalf("timedout waiting for two entries in the mount table")
	case <-ch:
	}

	got := []string{}
	for _, s := range verifyMount(t, ns, name) {
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
		ns.Unmount(testContext(), "mountpoint/server", sep)
	}

	addrs = verifyMount(t, ns, name)
	if len(addrs) != 1 {
		t.Fatalf("failed to lookup proxy: addrs %v", addrs)
	}

	// Proxied endpoint should be published and RPC should succeed (through proxy)
	const expected = `method:"Echo",suffix:"suffix",arg:"batman"`
	if result, err := makeCall(); result != expected || err != nil {
		t.Fatalf("Got (%v, %v) want (%v, nil)", result, err, expected)
	}
	// Proxy dies, calls should fail and the name should be unmounted.
	if err := proxy.Stop(); err != nil {
		t.Fatal(err)
	}

	if result, err := makeCall(); err == nil || (!strings.HasPrefix(err.Error(), "RESOLVE") && !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf(`Got (%v, %v) want ("", "RESOLVE: <err>" or "EOF") as proxy is down`, result, err)
	}

	for {
		if _, err := ns.Resolve(testContext(), name); err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	verifyMountMissing(t, ns, name)

	// Proxy restarts, calls should eventually start succeeding.
	if err := proxy.Start(t, args...); err != nil {
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

// Required by modules framework.
func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}
