package rt_test

import (
	"testing"

	"v.io/core/veyron2/context"

	"v.io/core/veyron/runtimes/google/rt"
	"v.io/core/veyron/security"
)

// InitForTest creates a context for use in a test.
// TODO(mattr): We should call runtimeX.Init once that is implemented.
func InitForTest(t *testing.T) (*context.T, context.CancelFunc) {
	rt, err := rt.New(profileOpt)
	if err != nil {
		t.Fatalf("Could not create runtime: %v", err)
	}
	ctx, cancel := context.WithCancel(rt.NewContext())
	go func() {
		<-ctx.Done()
		rt.Cleanup()
	}()
	return ctx, cancel
}

func TestNewServer(t *testing.T) {
	ctx, cancel := InitForTest(t)
	defer cancel()

	r := &rt.RuntimeX{}
	if s, err := r.NewServer(ctx); err != nil || s == nil {
		t.Fatalf("Could not create server: %v", err)
	}
}

func TestStreamManager(t *testing.T) {
	ctx, cancel := InitForTest(t)
	defer cancel()

	r := &rt.RuntimeX{}
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
	ctx, cancel := InitForTest(t)
	defer cancel()

	r := &rt.RuntimeX{}

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
	ctx, cancel := InitForTest(t)
	defer cancel()

	r := &rt.RuntimeX{}
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
	ctx, cancel := InitForTest(t)
	defer cancel()

	r := &rt.RuntimeX{}
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
