// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"testing"

	"v.io/v23/context"
	"v.io/v23/logging"
	"v.io/v23/security"
	wire "v.io/v23/services/syncbase"
)

func TestJoinSplitPatterns(t *testing.T) {
	cases := []struct {
		patterns []security.BlessingPattern
		joined   string
	}{
		{nil, ""},
		{[]security.BlessingPattern{"a", "b"}, "a,b"},
		{[]security.BlessingPattern{"a:b:c", "d:e:f"}, "a:b:c,d:e:f"},
		{[]security.BlessingPattern{"alpha:one", "alpha:two", "alpha:three"}, "alpha:one,alpha:two,alpha:three"},
	}
	for _, c := range cases {
		if got := joinPatterns(c.patterns); got != c.joined {
			t.Errorf("%#v, got %q, wanted %q", c.patterns, got, c.joined)
		}
		if got := splitPatterns(c.joined); !reflect.DeepEqual(got, c.patterns) {
			t.Errorf("%q, got %#v, wanted %#v", c.joined, got, c.patterns)
		}
	}
	// Special case, Joining an empty non-nil list results in empty string.
	if got := joinPatterns([]security.BlessingPattern{}); got != "" {
		t.Errorf("Joining empty list: got %q, want %q", got, "")
	}
}

type logger struct {
	logging.Logger
}

func (l logger) InfoDepth(depth int, args ...interface{}) {
	fmt.Fprintln(os.Stdout, args...)
}

func TestInviteQueue(t *testing.T) {
	ctx, cancel := context.RootContext()
	defer cancel()

	ctx = context.WithLogger(ctx, logger{})

	q := newInviteQueue()
	if q.size() != 0 {
		t.Errorf("got %d, want 0", q.size())
	}

	want := []string{"0", "1", "2", "3", "4"}

	// Test inserts during a long scan.
	ch := make(chan struct{})
	go func() {
		scan(ctx, q, want)
		close(ch)
	}()

	// Add a bunch of entries.
	for i, w := range want {
		q.add(Invite{Syncgroup: wire.Id{Name: w}, key: w})
		if q.size() != i+1 {
			t.Errorf("got %d, want %d", q.size(), i+1)
		}
		if err := scan(ctx, q, want[:i+1]); err != nil {
			t.Error(err)
		}
	}

	// Make sure long term scan finished.
	<-ch

	// Start another long scan that will proceed across deletes
	steps := make(chan struct{})
	go func() {
		ctx, cancel := context.WithCancel(ctx)
		c := q.scan()

		if inv, ok := q.next(ctx, c); !ok || inv.Syncgroup.Name != want[0] {
			t.Error("next should have suceeded.")
		}
		if inv, ok := q.next(ctx, c); !ok || inv.Syncgroup.Name != want[1] {
			t.Error("next should have suceeded.")
		}

		steps <- struct{}{}
		<-steps

		if inv, ok := q.next(ctx, c); !ok || inv.Syncgroup.Name != want[3] {
			t.Error("next should have suceeded.")
		}
		if inv, ok := q.next(ctx, c); !ok || inv.Syncgroup.Name != want[4] {
			t.Error("next should have suceeded.")
		}
		cancel()

		steps <- struct{}{}
	}()

	// Wait for the scan to read the first two values.
	<-steps

	// Remove a bunch of entries.
	for i, w := range want {
		q.remove(Invite{Syncgroup: wire.Id{Name: w}, key: w})
		if i == 2 {
			// Tell the scan to read the next two values
			steps <- struct{}{}
			// Wait for it to finish.
			<-steps
		}
		if q.size() != len(want)-i-1 {
			t.Errorf("got %d, want %d", q.size(), len(want)-i-1)
		}
		if err := scan(ctx, q, want[i+1:]); err != nil {
			t.Error(err)
		}
	}
}

func logList(ctx *context.T, q *inviteQueue) {
	buf := &bytes.Buffer{}
	for e := q.sentinel.next; e != &q.sentinel; e = e.next {
		fmt.Fprintf(buf, "%p ", e)
	}
	ctx.Info("list", buf.String())
}

func scan(ctx *context.T, q *inviteQueue, want []string) error {
	ctx, cancel := context.WithCancel(ctx)
	c := q.scan()
	for i, w := range want {
		inv, ok := q.next(ctx, c)
		if !ok {
			return fmt.Errorf("scan ended after %d entries, wanted %d", i, len(want))
		}
		if got := inv.Syncgroup.Name; got != w {
			return fmt.Errorf("got %s, want %s", got, w)
		}
		if i == len(want)-1 {
			// Calling cancel should allow next to fail, exiting the loop.
			cancel()
		}
	}
	return nil
}
