// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt_test

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/runtime/internal/rt"
	"v.io/x/ref/services/debug/debuglib"
	"v.io/x/ref/test/testutil"
)

// initForTest creates a context for use in a test.
func initForTest(t *testing.T) (*rt.Runtime, *context.T, v23.Shutdown) {
	ctx, cancel := context.RootContext()
	r, ctx, shutdown, err := rt.Init(ctx, nil, nil, nil, nil, "", flags.RuntimeFlags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = r.WithPrincipal(ctx, testutil.NewPrincipal("test-blessing")); err != nil {
		t.Fatal(err)
	}
	return r, ctx, func() {
		cancel()
		shutdown()
	}
}

func TestNewServer(t *testing.T) {
	r, ctx, shutdown := initForTest(t)
	defer shutdown()

	if s, err := r.NewServer(ctx); err != nil || s == nil {
		t.Fatalf("Could not create server: %v", err)
	}
}

func TestPrincipal(t *testing.T) {
	r, ctx, shutdown := initForTest(t)
	defer shutdown()

	p2 := testutil.NewPrincipal()
	c2, err := r.WithPrincipal(ctx, p2)
	if err != nil {
		t.Fatalf("Could not attach principal: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if p2 != r.GetPrincipal(c2) {
		t.Fatal("The new principal should be attached to the context, but it isn't")
	}
}

func TestClient(t *testing.T) {
	r, ctx, shutdown := initForTest(t)
	defer shutdown()

	orig := r.GetClient(ctx)

	c2, client, err := r.WithNewClient(ctx)
	if err != nil || client == nil {
		t.Fatalf("Could not create client: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if client == orig {
		t.Fatal("Should have replaced the client but didn't")
	}
	if client != r.GetClient(c2) {
		t.Fatal("The new client should be attached to the context, but it isn't")
	}
}

func TestNamespace(t *testing.T) {
	r, ctx, shutdown := initForTest(t)
	defer shutdown()

	orig := r.GetNamespace(ctx)
	orig.CacheCtl(naming.DisableCache(true))

	newroots := []string{"/newroot1", "/newroot2"}
	c2, ns, err := r.WithNewNamespace(ctx, newroots...)
	if err != nil || ns == nil {
		t.Fatalf("Could not create namespace: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if ns == orig {
		t.Fatal("Should have replaced the namespace but didn't")
	}
	if ns != r.GetNamespace(c2) {
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
	r, ctx, shutdown := initForTest(t)
	defer shutdown()

	bgctx := r.GetBackgroundContext(ctx)

	if bgctx == ctx {
		t.Error("The background context should not be the same as the context")
	}

	bgctx2 := r.GetBackgroundContext(bgctx)
	if bgctx != bgctx2 {
		t.Error("Calling GetBackgroundContext a second time should return the same context.")
	}
}

func TestReservedNameDispatcher(t *testing.T) {
	r, ctx, shutdown := initForTest(t)
	defer shutdown()

	oldDebugDisp := r.GetReservedNameDispatcher(ctx)
	newDebugDisp := debuglib.NewDispatcher(vlog.Log.LogDir, nil)

	nctx := r.WithReservedNameDispatcher(ctx, newDebugDisp)
	debugDisp := r.GetReservedNameDispatcher(nctx)

	if debugDisp != newDebugDisp || debugDisp == oldDebugDisp {
		t.Error("SetNewDebugDispatcher didn't update the context properly")
	}

}
