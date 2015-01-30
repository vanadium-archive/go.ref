package rt_test

import (
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	"v.io/core/veyron/lib/flags"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/runtimes/google/rt"
	"v.io/core/veyron/security"
)

// InitForTest creates a context for use in a test.
func InitForTest(t *testing.T) (*rt.Runtime, *context.T, veyron2.Shutdown) {
	ctx, cancel := context.RootContext()
	r, ctx, shutdown, err := rt.Init(ctx, nil, nil, nil, flags.RuntimeFlags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = r.SetPrincipal(ctx, tsecurity.NewPrincipal("test-blessing")); err != nil {
		t.Fatal(err)
	}
	return r, ctx, func() {
		cancel()
		shutdown()
	}
}

func TestNewServer(t *testing.T) {
	r, ctx, shutdown := InitForTest(t)
	defer shutdown()

	if s, err := r.NewServer(ctx); err != nil || s == nil {
		t.Fatalf("Could not create server: %v", err)
	}
}

func TestStreamManager(t *testing.T) {
	r, ctx, shutdown := InitForTest(t)
	defer shutdown()

	orig := r.GetStreamManager(ctx)

	c2, sm, err := r.SetNewStreamManager(ctx)
	if err != nil || sm == nil {
		t.Fatalf("Could not create stream manager: %v", err)
	}
	if !c2.Initialized() {
		t.Fatal("Got uninitialized context.")
	}
	if sm == orig {
		t.Fatal("Should have replaced the stream manager but didn't")
	}
	if sm != r.GetStreamManager(c2) {
		t.Fatal("The new stream manager should be attached to the context, but it isn't")
	}
}

func TestPrincipal(t *testing.T) {
	r, ctx, shutdown := InitForTest(t)
	defer shutdown()

	p2, err := security.NewPrincipal()
	if err != nil {
		t.Fatalf("Could not create new principal %v", err)
	}
	c2, err := r.SetPrincipal(ctx, p2)
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
	r, ctx, shutdown := InitForTest(t)
	defer shutdown()

	orig := r.GetClient(ctx)

	c2, client, err := r.SetNewClient(ctx)
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
	r, ctx, shutdown := InitForTest(t)
	defer shutdown()

	orig := r.GetNamespace(ctx)

	newroots := []string{"/newroot1", "/newroot2"}
	c2, ns, err := r.SetNewNamespace(ctx, newroots...)
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
}

func TestBackgroundContext(t *testing.T) {
	r, ctx, shutdown := InitForTest(t)
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
