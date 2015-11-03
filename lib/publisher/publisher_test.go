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

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/namespace"

	"v.io/x/ref/lib/publisher"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

//go:generate jiri test generate

func resolveWithRetry(t *testing.T, ns namespace.T, ctx *context.T, name string, expected int) []string {
	deadline := time.Now().Add(10 * time.Second)
	for {
		me, err := ns.Resolve(ctx, name)
		if err == nil && len(me.Names()) == expected {
			return me.Names()
		}
		if time.Now().After(deadline) {
			t.Fatalf("failed to resolve %q", name)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func verifyMissing(t *testing.T, ns namespace.T, ctx *context.T, name string) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := ns.Resolve(ctx, "foo"); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q is still mounted", name)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestAddAndRemove(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()
	ns := v23.GetNamespace(ctx)
	pub := publisher.New(ctx, ns, time.Second)
	pub.AddName("foo", false, false)
	pub.AddServer("foo:8000")
	if got, want := resolveWithRetry(t, ns, ctx, "foo", 1), []string{"/foo:8000"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddServer("bar:8000")
	got, want := resolveWithRetry(t, ns, ctx, "foo", 2), []string{"/bar:8000", "/foo:8000"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.AddName("baz", false, false)
	got = resolveWithRetry(t, ns, ctx, "baz", 2)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.RemoveName("foo")
	verifyMissing(t, ns, ctx, "foo")
	pub.Stop()
	pub.WaitForStop()
}

func TestStatus(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()
	ns := v23.GetNamespace(ctx)
	pub := publisher.New(ctx, ns, time.Second)
	pub.AddName("foo", false, false)
	status := pub.Status()
	if got, want := len(status), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	pub.AddServer("foo:8000")

	// Wait for the publisher to asynchronously publish the
	// requisite number of servers.
	ch := make(chan error, 1)
	waitFor := func(n int) {
		deadline := time.Now().Add(time.Minute)
		for {
			status = pub.Status()
			if got, want := len(status), n; got != want {
				if time.Now().After(deadline) {
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

	pub.AddServer("bar:8000")
	pub.AddName("baz", false, false)

	go waitFor(4)
	if err := <-ch; err != nil {
		t.Fatalf("%s", err)
	}

	status = pub.Status()
	names := status.Names()
	if got, want := names, []string{"baz", "foo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	servers := status.Servers()
	if got, want := servers, []string{"bar:8000", "foo:8000"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	pub.RemoveName("foo")
	verifyMissing(t, ns, ctx, "foo")

	status = pub.Status()
	go waitFor(2)
	if err := <-ch; err != nil {
		t.Fatalf("%s", err)
	}
	pub.Stop()
	pub.WaitForStop()
}
