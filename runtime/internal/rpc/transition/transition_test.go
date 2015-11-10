// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transition

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/runtime/factories/generic"
	irpc "v.io/x/ref/runtime/internal/rpc"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/services/xproxy/xproxy"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type example struct{}

func (e *example) Echo(ctx *context.T, call rpc.ServerCall, arg string) (string, error) {
	return arg, nil
}

func TestTransitionToNew(t *testing.T) {
	// TODO(mattr): Make a test showing the transition client
	// connecting to a new server once the new server is available.
}

func TestTransitionToOld(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	sm := manager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()

	sp := testutil.NewPrincipal()
	testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx)).Bless(sp, "server")
	server, err := irpc.DeprecatedNewServer(ctx, sm, v23.GetNamespace(ctx),
		nil, "", v23.GetClient(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.Listen(v23.GetListenSpec(ctx)); err != nil {
		t.Fatal(err)
	}
	if err = server.Serve("echo", &example{}, nil); err != nil {
		t.Fatal(err)
	}

	var result string
	err = v23.GetClient(ctx).Call(ctx, "echo", "Echo",
		[]interface{}{"hello"}, []interface{}{&result})
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("got %s, wanted hello", result)
	}
}

func TestTransitionThroughProxy(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	var (
		proxyName  = "proxy"
		serverName = "echo"
	)

	pctx, cancel := context.WithCancel(ctx)
	proxy, err := xproxy.New(pctx, proxyName, security.AllowEveryone())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		<-proxy.Closed()
	}()

	// Start an old proxy.
	proxyShutdown, _, err := generic.NewProxy(ctx, v23.GetListenSpec(ctx), security.AllowEveryone(), proxyName)
	if err != nil {
		t.Fatal(err)
	}
	defer proxyShutdown()

	sm := manager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	lspec := rpc.ListenSpec{Proxy: proxyName}
	ctx = v23.WithListenSpec(ctx, lspec)
	// Test an old server listening on the proxy.
	server, err := irpc.DeprecatedNewServer(ctx, sm, v23.GetNamespace(ctx),
		nil, "", v23.GetClient(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.Listen(lspec); err != nil {
		t.Fatal(err)
	}
	if err = server.Serve(serverName, &example{}, nil); err != nil {
		t.Fatal(err)
	}

	var result string
	err = v23.GetClient(ctx).Call(ctx, serverName, "Echo",
		[]interface{}{"hello"}, []interface{}{&result})
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("got %s, wanted hello", result)
	}

	// Test a new server listening on the proxy.
	if _, _, err = irpc.WithNewServer(ctx, serverName, &example{}, nil, nil); err != nil {
		t.Fatal(err)
	}

	result = ""
	err = v23.GetClient(ctx).Call(ctx, serverName, "Echo",
		[]interface{}{"hello"}, []interface{}{&result})
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("got %s, wanted hello", result)
	}
}
