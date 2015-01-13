package publisher_test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/vtrace"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/runtimes/google/lib/publisher"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

func testContext() *context.T {
	var ctx *context.T
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{})
	if err != nil {
		panic(err)
	}
	ctx, _ = vtrace.SetNewSpan(ctx, "")
	ctx, _ = context.WithDeadline(ctx, time.Now().Add(20*time.Second))
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
