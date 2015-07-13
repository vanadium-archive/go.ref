// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/flags"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/internal/lib/publisher"
	inaming "v.io/x/ref/runtime/internal/naming"
	irpc "v.io/x/ref/runtime/internal/rpc"
	imanager "v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/runtime/internal/rpc/stream/proxy"
	tnaming "v.io/x/ref/runtime/internal/testing/mocks/naming"
	ivtrace "v.io/x/ref/runtime/internal/vtrace"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

func testContext() (*context.T, func()) {
	ctx, shutdown := v23.Init()
	ctx, _ = context.WithTimeout(ctx, 20*time.Second)
	var err error
	if ctx, err = ivtrace.Init(ctx, flags.VtraceFlags{}); err != nil {
		panic(err)
	}
	ctx, _ = vtrace.WithNewTrace(ctx)
	return ctx, shutdown
}

var proxyServer = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	expected := len(args)

	listenSpec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	proxyShutdown, proxyEp, err := proxy.New(ctx, listenSpec, security.AllowEveryone())
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s\n", verror.DebugString(err))
		return err
	}
	defer proxyShutdown()
	fmt.Fprintf(env.Stdout, "PID=%d\n", os.Getpid())
	if expected > 0 {
		pub := publisher.New(ctx, v23.GetNamespace(ctx), time.Minute)
		defer pub.WaitForStop()
		defer pub.Stop()
		pub.AddServer(proxyEp.String())
		for _, name := range args {
			if len(name) == 0 {
				return fmt.Errorf("empty name specified on the command line")
			}
			pub.AddName(name, false, false)
		}
		// Wait for all the entries to be published.
		for {
			pubState := pub.Status()
			if expected == len(pubState) {
				break
			}
			delay := time.Second
			time.Sleep(delay)
		}
	}
	fmt.Fprintf(env.Stdout, "PROXY_NAME=%s\n", proxyEp.Name())
	modules.WaitForEOF(env.Stdin)
	fmt.Fprintf(env.Stdout, "DONE\n")
	return nil
}, "")

type testServer struct{}

func (*testServer) Echo(_ *context.T, call rpc.ServerCall, arg string) (string, error) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", "Echo", call.Suffix(), arg), nil
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(*context.T, security.Call) error {
	return nil
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return t.server, testServerAuthorizer{}, nil
}

type proxyHandle struct {
	ns    namespace.T
	sh    *modules.Shell
	proxy modules.Handle
	name  string
}

func (h *proxyHandle) Start(t *testing.T, ctx *context.T, args ...string) error {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.sh = sh
	p, err := sh.Start(nil, proxyServer, args...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.proxy = p
	p.ReadLine()
	h.name = p.ExpectVar("PROXY_NAME")
	if len(h.name) == 0 {
		h.proxy.Shutdown(os.Stderr, os.Stderr)
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
	listenSpec := rpc.ListenSpec{Proxy: "proxy"}
	testProxy(t, listenSpec)
}

func TestProxy(t *testing.T) {
	proxyListenSpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}},
		Proxy: "proxy",
	}
	testProxy(t, proxyListenSpec)
}

func TestWSProxy(t *testing.T) {
	proxyListenSpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}},
		Proxy: "proxy",
	}
	// The proxy uses websockets only, but the server is using tcp.
	testProxy(t, proxyListenSpec, "--v23.tcp.protocol=ws")
}

func testProxy(t *testing.T, spec rpc.ListenSpec, args ...string) {
	ctx, shutdown := testContext()
	defer shutdown()

	var (
		pserver   = testutil.NewPrincipal("server")
		pclient   = testutil.NewPrincipal("client")
		serverKey = pserver.PublicKey()
		// We use different stream managers for the client and server
		// to prevent VIF re-use (in other words, we want to test VIF
		// creation from both the client and server end).
		smserver = imanager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
		smclient = imanager.InternalNew(ctx, naming.FixedRoutingID(0x444444444))
		ns       = tnaming.NewSimpleNamespace()
	)
	defer smserver.Shutdown()
	defer smclient.Shutdown()
	client, err := irpc.InternalNewClient(smserver, ns)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverCtx, _ := v23.WithPrincipal(ctx, pserver)
	server, err := irpc.InternalNewServer(serverCtx, smserver, ns, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// The client must recognize the server's blessings, otherwise it won't
	// communicate with it.
	pclient.AddToRoots(pserver.BlessingStore().Default())

	// If no address is specified then we'll only 'listen' via
	// the proxy.
	hasLocalListener := len(spec.Addrs) > 0 && len(spec.Addrs[0].Address) != 0

	name := "mountpoint/server/suffix"
	makeCall := func(opts ...rpc.CallOpt) (string, error) {
		clientCtx, _ := v23.WithPrincipal(ctx, pclient)
		clientCtx, _ = context.WithDeadline(clientCtx, time.Now().Add(5*time.Second))
		call, err := client.StartCall(clientCtx, name, "Echo", []interface{}{"batman"}, opts...)
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
			if err != nil {
				continue
			}
			for i, s := range me.Servers {
				ctx.Infof("%d: %s", i, s)
			}
			if err == nil && len(me.Servers) == expect {
				ch <- 1
				return
			}
			if time.Now().After(then) {
				t.Fatalf("timed out waiting for %d servers, found %d", expect, len(me.Servers))
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
	proxiedEP.Blessings = []string{"server"}
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

	// Proxied endpoint should be published and RPC should succeed (through proxy).
	// Additionally, any server authorizaton options must only apply to the end server
	// and not the proxy.
	const expected = `method:"Echo",suffix:"suffix",arg:"batman"`
	if result, err := makeCall(options.ServerPublicKey{serverKey}); result != expected || err != nil {
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
	if got, want := len(status.Proxies), 1; got != want {
		t.Logf("Proxies: %v", status.Proxies)
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := status.Proxies[0].Proxy, spec.Proxy; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := verror.ErrorID(status.Proxies[0].Error), verror.ErrNoServers.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
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

func verifyMount(t *testing.T, ctx *context.T, ns namespace.T, name string) []string {
	me, err := ns.Resolve(ctx, name)
	if err != nil {
		t.Errorf("%s not found in mounttable", name)
		return nil
	}
	return me.Names()
}

func verifyMountMissing(t *testing.T, ctx *context.T, ns namespace.T, name string) {
	if me, err := ns.Resolve(ctx, name); err == nil {
		names := me.Names()
		t.Errorf("%s not supposed to be found in mounttable; got %d servers instead: %v", name, len(names), names)
	}
}
