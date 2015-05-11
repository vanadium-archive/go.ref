// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/namespace"
	"v.io/v23/vtrace"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/runtime/internal/lib/publisher"
	tnaming "v.io/x/ref/runtime/internal/testing/mocks/naming"
	ivtrace "v.io/x/ref/runtime/internal/vtrace"
)

//go:generate v23 test generate

func testContext() *context.T {
	ctx, _ := context.RootContext()
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{})
	if err != nil {
		panic(err)
	}
	ctx, _ = vtrace.WithNewSpan(ctx, "")
	ctx, _ = context.WithDeadline(ctx, time.Now().Add(20*time.Second))
	return ctx
}

func resolve(t *testing.T, ns namespace.T, name string) []string {
	me, err := ns.Resolve(testContext(), name)
	if err != nil {
		t.Fatalf("failed to resolve %q", name)
	}
	return me.Names()
}

func TestAddAndRemove(t *testing.T) {
	ns := tnaming.NewSimpleNamespace()
	pub := publisher.New(testContext(), ns, time.Second)
	pub.AddName("foo", false, false)
	pub.AddServer("foo-addr")
	if got, want := resolve(t, ns, "foo"), []string{"/foo-addr"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddServer("bar-addr")
	got, want := resolve(t, ns, "foo"), []string{"/bar-addr", "/foo-addr"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddName("baz", false, false)
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
	pub.AddName("foo", false, false)
	status := pub.Status()
	if got, want := len(status), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	pub.AddServer("foo-addr")

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

	pub.AddServer("bar-addr")
	pub.AddName("baz", false, false)
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
