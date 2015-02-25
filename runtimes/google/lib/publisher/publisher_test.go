package publisher_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming/ns"
	"v.io/v23/vtrace"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/runtimes/google/lib/publisher"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

//go:generate v23 test generate

func testContext() *context.T {
	ctx, _ := context.RootContext()
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{})
	if err != nil {
		panic(err)
	}
	ctx, _ = vtrace.SetNewSpan(ctx, "")
	ctx, _ = context.WithDeadline(ctx, time.Now().Add(20*time.Second))
	return ctx
}

func resolve(t *testing.T, ns ns.Namespace, name string) []string {
	me, err := ns.Resolve(testContext(), name)
	if err != nil {
		t.Fatalf("failed to resolve %q", name)
	}
	return me.Names()
}

func TestAddAndRemove(t *testing.T) {
	ns := tnaming.NewSimpleNamespace()
	pub := publisher.New(testContext(), ns, time.Second)
	pub.AddName("foo")
	pub.AddServer("foo-addr", false)
	if got, want := resolve(t, ns, "foo"), []string{"/foo-addr"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddServer("bar-addr", false)
	got, want := resolve(t, ns, "foo"), []string{"/bar-addr", "/foo-addr"}
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

func TestStatus(t *testing.T) {
	ns := tnaming.NewSimpleNamespace()
	pub := publisher.New(testContext(), ns, time.Second)
	pub.AddName("foo")
	status := pub.Status()
	if got, want := len(status), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	pub.AddServer("foo-addr", false)

	// Wait for the publisher to asynchronously publish server the
	// requisite number of servers.
	ch := make(chan error, 1)
	waitFor := func(n int) {
		then := time.Now()
		for {
			status = pub.Status()
			if got, want := len(status), n; got != want {
				if time.Now().Sub(then) > time.Minute {
					ch <- fmt.Errorf("got %d, want %d", got, want)
					return
				}
				time.Sleep(100 * time.Millisecond)
			} else {
				ch <- nil
				return
			}
		}
	}

	go waitFor(1)
	if err := <-ch; err != nil {
		t.Fatalf("%s", err)
	}

	pub.AddServer("bar-addr", false)
	pub.AddName("baz")
	status = pub.Status()
	names := status.Names()
	if got, want := names, []string{"baz", "foo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	servers := status.Servers()
	if got, want := servers, []string{"bar-addr", "foo-addr"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.RemoveName("foo")
	if _, err := ns.Resolve(testContext(), "foo"); err == nil {
		t.Errorf("expected an error")
	}
	status = pub.Status()

	go waitFor(2)
	if err := <-ch; err != nil {
		t.Fatalf("%s", err)
	}
}
