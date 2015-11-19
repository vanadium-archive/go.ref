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
	"v.io/x/ref"
	"v.io/x/ref/lib/publisher"
	_ "v.io/x/ref/runtime/factories/generic"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream/proxy"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

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
	if ref.RPCTransitionState() >= ref.XServers {
		// This test cannot pass under the new RPC system.  It expects
		// to distinguish between proxy endpoints and non-proxy endpoints
		// which the new system does not support.
		t.SkipNow()
	}
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	var (
		pserver   = testutil.NewPrincipal()
		pclient   = testutil.NewPrincipal()
		serverKey = pserver.PublicKey()
		ns        = v23.GetNamespace(ctx)
	)
	idp := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	idp.Bless(pserver, "server")
	idp.Bless(pclient, "client")
	clientCtx, err := v23.WithPrincipal(ctx, pclient)
	if err != nil {
		t.Fatal(err)
	}
	client := v23.GetClient(clientCtx)

	serverCtx, err := v23.WithPrincipal(v23.WithListenSpec(ctx, spec), pserver)
	if err != nil {
		t.Fatal(err)
	}
	_, server, err := v23.WithNewDispatchingServer(
		serverCtx,
		"mountpoint/server",
		testServerDisp{&testServer{}})
	if err != nil {
		t.Fatal(err)
	}

	// The client must recognize the server's blessings, otherwise it won't
	// communicate with it.
	security.AddToRoots(pclient, pserver.BlessingStore().Default())

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

	proxyAddr, _ := naming.SplitAddressName(addrs[0])
	proxyEP, err := inaming.NewEndpoint(proxyAddr)
	if err != nil {
		t.Fatalf("unexpected error for %q: %s", proxyEP, err)
	}

	// Proxy connetions are created asynchronously, so we wait for the
	// expected number of endpoints to appear for the specified service name.
	ch := make(chan struct{})
	numNames := 1
	if hasLocalListener {
		numNames = 2
	}
	// Proxy connections are started asynchronously, so we need to wait..
	go func() {
		then := time.Now().Add(time.Minute)
		for {
			me, err := ns.Resolve(ctx, name)
			if err != nil {
				continue
			}
			for i, s := range me.Servers {
				ctx.Infof("%d: %s", i, s)
			}
			if err == nil && len(me.Servers) == numNames {
				close(ch)
				return
			}
			if time.Now().After(then) {
				t.Fatalf("timed out waiting for %d servers, found %d", numNames, len(me.Servers))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-time.After(time.Minute):
		t.Fatalf("timedout waiting for two entries in the mount table and server status")
	case <-ch:
	}

	status := server.Status()
	proxiedEP := status.Proxies[0].Endpoint
	if proxiedEP.Network() != proxyEP.Network() ||
		proxiedEP.Addr().String() != proxyEP.Addr().String() ||
		proxiedEP.ServesMountTable() || proxiedEP.ServesLeaf() ||
		proxiedEP.BlessingNames()[0] != "test-blessing:server" {
		t.Fatalf("got %q, want (tcp, %s, s, test-blessing:server)",
			proxiedEP, proxyEP.Addr().String())
	}
	expectedNames := []string{naming.JoinAddressName(proxiedEP.String(), "suffix")}
	if hasLocalListener {
		normalEP := status.Endpoints[0]
		expectedNames = append(expectedNames, naming.JoinAddressName(normalEP.String(), "suffix"))
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
		sep := status.Endpoints[0].String()
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
	if result, err := makeCall(options.ServerAuthorizer{security.PublicKeyAuthorizer(serverKey)}); result != expected || err != nil {
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
