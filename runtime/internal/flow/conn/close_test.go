// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"bytes"
	"fmt"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test/goroutines"
)

func TestRemoteDialerClose(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	d, a, w := setupConns(t, ctx, ctx, nil, nil)
	d.Close(ctx, fmt.Errorf("Closing randomly."))
	<-d.Closed()
	<-a.Closed()
	if !w.IsClosed() {
		t.Errorf("The connection should be closed")
	}
}

func TestRemoteAcceptorClose(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	d, a, w := setupConns(t, ctx, ctx, nil, nil)
	a.Close(ctx, fmt.Errorf("Closing randomly."))
	<-a.Closed()
	<-d.Closed()
	if !w.IsClosed() {
		t.Errorf("The connection should be closed")
	}
}

func TestUnderlyingConnectionClosed(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	d, a, w := setupConns(t, ctx, ctx, nil, nil)
	w.Close()
	<-a.Closed()
	<-d.Closed()
}

func TestDialAfterConnClose(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	d, a, _ := setupConns(t, ctx, ctx, nil, nil)

	d.Close(ctx, fmt.Errorf("Closing randomly."))
	<-d.Closed()
	<-a.Closed()
	if _, err := d.Dial(ctx, testBFP); err == nil {
		t.Errorf("Nil error dialing on dialer")
	}
	if _, err := a.Dial(ctx, testBFP); err == nil {
		t.Errorf("Nil error dialing on acceptor")
	}
}

func TestReadWriteAfterConnClose(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	for _, dialerDials := range []bool{true, false} {
		df, flows, cl := setupFlow(t, ctx, ctx, dialerDials)
		if _, err := df.WriteMsg([]byte("hello")); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		af := <-flows
		if got, err := af.ReadMsg(); err != nil {
			t.Fatalf("read failed: %v", err)
		} else if !bytes.Equal(got, []byte("hello")) {
			t.Errorf("got %s want %s", string(got), "hello")
		}
		if _, err := df.WriteMsg([]byte("there")); err != nil {
			t.Fatalf("second write failed: %v", err)
		}
		df.(*flw).conn.Close(ctx, fmt.Errorf("Closing randomly."))
		<-af.Conn().Closed()
		if got, err := af.ReadMsg(); err != nil {
			t.Fatalf("read failed: %v", err)
		} else if !bytes.Equal(got, []byte("there")) {
			t.Errorf("got %s want %s", string(got), "there")
		}
		if _, err := df.WriteMsg([]byte("fail")); err == nil {
			t.Errorf("nil error for write after close.")
		}
		if _, err := af.ReadMsg(); err == nil {
			t.Fatalf("nil error for read after close.")
		}
		cl()
	}
}

func TestFlowCancelOnWrite(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	dctx, cancel := context.WithCancel(ctx)
	df, accept, cl := setupFlow(t, dctx, ctx, true)
	defer cl()
	done := make(chan struct{})
	go func() {
		if _, err := df.WriteMsg([]byte("hello")); err != nil {
			t.Fatalf("could not write flow: %v", err)
		}
		for {
			if _, err := df.WriteMsg([]byte("hello")); err == context.Canceled {
				break
			} else if err != nil {
				t.Fatalf("unexpected error waiting for cancel: %v", err)
			}
		}
		close(done)
	}()
	af := <-accept
	cancel()
	<-done
	<-af.Closed()
}

func TestFlowCancelOnRead(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	defer shutdown()
	dctx, cancel := context.WithCancel(ctx)
	df, accept, cl := setupFlow(t, dctx, ctx, true)
	defer cl()
	done := make(chan struct{})
	go func() {
		if _, err := df.WriteMsg([]byte("hello")); err != nil {
			t.Fatalf("could not write flow: %v", err)
		}
		if _, err := df.ReadMsg(); err != context.Canceled {
			t.Fatalf("unexpected error waiting for cancel: %v", err)
		}
		close(done)
	}()
	af := <-accept
	cancel()
	<-done
	<-af.Closed()
}
