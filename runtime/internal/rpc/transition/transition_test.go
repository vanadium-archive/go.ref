// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transition

import (
	"fmt"
	"sync"
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

type dischargeService struct {
	called int
	mu     sync.Mutex
}

func (ds *dischargeService) Discharge(ctx *context.T, call rpc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("discharger: not a third party caveat (%v)", cav)
	}
	if err := tp.Dischargeable(ctx, call.Security()); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", tp, err)
	}
	caveat := security.UnconstrainedUse()
	return call.Security().LocalPrincipal().MintDischarge(cav, caveat)
}

func mkThirdPartyCaveat(discharger security.PublicKey, location string, caveats ...security.Caveat) security.Caveat {
	if len(caveats) == 0 {
		caveats = []security.Caveat{security.UnconstrainedUse()}
	}
	tpc, err := security.NewPublicKeyCaveat(discharger, location, security.ThirdPartyRequirements{}, caveats[0], caveats[1:]...)
	if err != nil {
		panic(err)
	}
	return tpc
}

func TestTransitionToOldNewDischarger(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	sm := manager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()

	idp := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	sp := testutil.NewPrincipal()
	idp.Bless(sp, "server")
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

	dp := testutil.NewPrincipal()
	idp.Bless(dp, "discharger")
	dctx, err := v23.WithPrincipal(ctx, dp)
	if err != nil {
		panic(err)
	}
	_, _, err = v23.WithNewServer(dctx, "discharge", &dischargeService{}, security.AllowEveryone())
	if err != nil {
		panic(err)
	}

	if err := idp.Bless(v23.GetPrincipal(ctx), "caveats", mkThirdPartyCaveat(dp.PublicKey(), "discharge")); err != nil {
		panic(err)
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

func TestTransitionToOld(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	sm := manager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()

	idp := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	sp := testutil.NewPrincipal()
	idp.Bless(sp, "server")
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
