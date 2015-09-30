// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/flow"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	"v.io/x/ref/test/goroutines"
)

func TestLameDuck(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()

	events := make(chan StatusUpdate, 2)
	dflows, aflows := make(chan flow.Flow, 3), make(chan flow.Flow, 3)
	dc, ac, _ := setupConnsWithEvents(t, ctx, ctx, dflows, aflows, events)

	go func() {
		for {
			select {
			case f := <-aflows:
				if got, err := f.ReadMsg(); err != nil {
					panic(fmt.Sprintf("got %v wanted nil", err))
				} else if !bytes.Equal(got, []byte("hello")) {
					panic(fmt.Sprintf("got %q, wanted 'hello'", string(got)))
				}
			case <-ac.Closed():
				return
			}
		}
	}()

	// Dial a flow and write it (which causes it to open).
	f1, err := dc.Dial(ctx, flowtest.AllowAllPeersAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f1.WriteMsg([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	// Dial more flows, but don't write to them yet.
	f2, err := dc.Dial(ctx, flowtest.AllowAllPeersAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	f3, err := dc.Dial(ctx, flowtest.AllowAllPeersAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}

	// Now put the accepted conn into lame duck mode.
	ac.EnterLameDuck(ctx)
	if e := <-events; e.Conn != dc || e.Status != (Status{false, false, true}) {
		t.Errorf("Expected RemoteLameDuck on dialer, got %#v (a %p, d %p)", e, ac, dc)
	}

	// Now we shouldn't be able to dial from dc because it's in lame duck mode.
	if _, err := dc.Dial(ctx, flowtest.AllowAllPeersAuthorizer{}); err == nil {
		t.Fatalf("expected an error, got nil")
	}

	// I can't think of a non-flaky way to test for it, but it should
	// be the case that we don't send the AckLameDuck message until
	// we write to or close the other flows.  This should catch it sometimes.
	select {
	case e := <-events:
		t.Errorf("Didn't expect any additional events yet, got %#v", e)
	case <-time.After(time.Millisecond * 100):
	}

	// Now write or close the other flows.
	if _, err := f2.WriteMsg([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	f3.Close()

	if e := <-events; e.Conn != ac || e.Status != (Status{false, true, false}) {
		t.Errorf("Expected LocalLameDuck on acceptor, got %#v (a %p, d %p)", e, ac, dc)
	}

	// Now put the dialer side into lame duck.
	dc.EnterLameDuck(ctx)
	if e := <-events; e.Conn != ac || e.Status != (Status{false, true, true}) {
		t.Errorf("Expected RemoteLameDuck on acceptor, got %#v (a %p, d %p)", e, ac, dc)
	}
	if e := <-events; e.Conn != dc || e.Status != (Status{false, true, true}) {
		t.Errorf("Expected LocalLameDuck on dialer, got %#v (a %p, d %p)", e, ac, dc)
	}

	// Now close the accept side.
	ac.Close(ctx, nil)
	if e := <-events; e.Status != (Status{true, true, true}) {
		t.Errorf("Expected Closed got %#v (a %p, d %p)", e, ac, dc)
	}
	if e := <-events; e.Status != (Status{true, true, true}) {
		t.Errorf("Expected Closed got %#v (a %p, d %p)", e, ac, dc)
	}
	<-dc.Closed()
	<-ac.Closed()
}
