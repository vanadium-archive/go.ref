package publisher_test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"

	iipc "v.io/core/veyron/runtimes/google/ipc"
	"v.io/core/veyron/runtimes/google/lib/publisher"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
	"v.io/core/veyron/runtimes/google/testing/mocks/runtime"
	"v.io/core/veyron/runtimes/google/vtrace"
)

func testContext() context.T {
	ctx := iipc.InternalNewContext(&runtime.PanicRuntime{})
	ctx, _ = vtrace.WithNewSpan(ctx, "")
	ctx, _ = ctx.WithDeadline(time.Now().Add(20 * time.Second))
	return ctx
}

func resolve(t *testing.T, ns naming.Namespace, name string) []string {
	servers, err := ns.Resolve(testContext(), name)
	if err != nil {
		t.Fatalf("failed to resolve %q", name)
	}
	return servers
}

func TestAddAndRemove(t *testing.T) {
	ns := tnaming.NewSimpleNamespace()
	pub := publisher.New(testContext(), ns, time.Second)
	pub.AddName("foo")
	pub.AddServer("foo-addr", false)
	if got, want := resolve(t, ns, "foo"), []string{"foo-addr"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddServer("bar-addr", false)
	got, want := resolve(t, ns, "foo"), []string{"bar-addr", "foo-addr"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddName("baz")
	got = resolve(t, ns, "baz")
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.RemoveName("foo")
	if _, err := ns.Resolve(testContext(), "foo"); err == nil {
		t.Errorf("expected an error")
	}
}
