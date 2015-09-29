// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt_test

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/debug/debuglib"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type fakeServer struct{}

func (*fakeServer) Foo(ctx *context.T, call rpc.ServerCall) error { return nil }

func TestWithNewServer(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	if _, s, err := v23.WithNewServer(ctx, "", &fakeServer{}, nil); err != nil || s == nil {
		t.Fatalf("Could not create server: %v", err)
	}
}

func TestPrincipal(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	p2 := testutil.NewPrincipal()
	c2, err := v23.WithPrincipal(ctx, p2)
	if err != nil {
		t.Fatalf("Could not attach principal: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if p2 != v23.GetPrincipal(c2) {
		t.Fatal("The new principal should be attached to the context, but it isn't")
	}
}

func TestClient(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	orig := v23.GetClient(ctx)

	c2, client, err := v23.WithNewClient(ctx)
	if err != nil || client == nil {
		t.Fatalf("Could not create client: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if client == orig {
		t.Fatal("Should have replaced the client but didn't")
	}
	if client != v23.GetClient(c2) {
		t.Fatal("The new client should be attached to the context, but it isn't")
	}
}

func TestNamespace(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	orig := v23.GetNamespace(ctx)
	orig.CacheCtl(naming.DisableCache(true))

	newroots := []string{"/newroot1", "/newroot2"}
	c2, ns, err := v23.WithNewNamespace(ctx, newroots...)
	if err != nil || ns == nil {
		t.Fatalf("Could not create namespace: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if ns == orig {
		t.Fatal("Should have replaced the namespace but didn't")
	}
	if ns != v23.GetNamespace(c2) {
		t.Fatal("The new namespace should be attached to the context, but it isn't")
	}
	newrootmap := map[string]bool{"/newroot1": true, "/newroot2": true}
	for _, root := range ns.Roots() {
		if !newrootmap[root] {
			t.Errorf("root %s found in ns, but we expected: %v", root, newroots)
		}
	}
	opts := ns.CacheCtl()
	if len(opts) != 1 {
		t.Fatalf("Expected one option for cache control, got %v", opts)
	}
	if disable, ok := opts[0].(naming.DisableCache); !ok || !bool(disable) {
		t.Errorf("expected a disable(true) message got %#v", opts[0])
	}
}

func TestBackgroundContext(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	bgctx := v23.GetBackgroundContext(ctx)

	if bgctx == ctx {
		t.Error("The background context should not be the same as the context")
	}

	bgctx2 := v23.GetBackgroundContext(bgctx)
	if bgctx != bgctx2 {
		t.Error("Calling GetBackgroundContext a second time should return the same context.")
	}
}

func TestReservedNameDispatcher(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	oldDebugDisp := v23.GetReservedNameDispatcher(ctx)
	newDebugDisp := debuglib.NewDispatcher(nil)

	nctx := v23.WithReservedNameDispatcher(ctx, newDebugDisp)
	debugDisp := v23.GetReservedNameDispatcher(nctx)

	if debugDisp != newDebugDisp || debugDisp == oldDebugDisp {
		t.Error("WithNewDebugDispatcher didn't update the context properly")
	}

}

func TestFlowManager(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	oldman, err := v23.NewFlowManager(ctx)
	if err != nil || oldman == nil {
		t.Error("NewFlowManager failed: %v, %v", oldman, err)
	}
	newman, err := v23.NewFlowManager(ctx)
	if err != nil || newman == nil || newman == oldman {
		t.Fatalf("NewFlowManager failed: %v, %v", newman, err)
	}
}
