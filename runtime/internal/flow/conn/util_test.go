// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"io"
	"sync"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
)

type wire struct {
	ctx    *context.T
	mu     sync.Mutex
	c      *sync.Cond
	closed bool
}

func (w *wire) close() {
	w.mu.Lock()
	w.closed = true
	w.c.Broadcast()
	w.mu.Unlock()
}

func (w *wire) isClosed() bool {
	w.mu.Lock()
	c := w.closed
	w.mu.Unlock()
	return c
}

type mRW struct {
	wire *wire
	in   []byte
	peer *mRW
}

func newMRWPair(ctx *context.T) (MsgReadWriteCloser, MsgReadWriteCloser, *wire) {
	w := &wire{ctx: ctx}
	w.c = sync.NewCond(&w.mu)
	a, b := &mRW{wire: w}, &mRW{wire: w}
	a.peer, b.peer = b, a
	return a, b, w
}

func (f *mRW) WriteMsg(data ...[]byte) (int, error) {
	buf := []byte{}
	for _, d := range data {
		buf = append(buf, d...)
	}
	defer f.wire.mu.Unlock()
	f.wire.mu.Lock()
	for f.peer.in != nil && !f.wire.closed {
		f.wire.c.Wait()
	}
	if f.wire.closed {
		return 0, io.EOF
	}
	f.peer.in = buf
	f.wire.c.Broadcast()
	return len(buf), nil
}
func (f *mRW) ReadMsg() (buf []byte, err error) {
	defer f.wire.mu.Unlock()
	f.wire.mu.Lock()
	for f.in == nil && !f.wire.closed {
		f.wire.c.Wait()
	}
	if f.wire.closed {
		return nil, io.EOF
	}
	buf, f.in = f.in, nil
	f.wire.c.Broadcast()
	return buf, nil
}
func (f *mRW) Close() error {
	f.wire.close()
	return nil
}

type fh chan<- flow.Flow

func (fh fh) HandleFlow(f flow.Flow) error {
	if fh == nil {
		panic("writing to nil flow handler")
	}
	fh <- f
	return nil
}

func setupConns(t *testing.T, ctx *context.T, dflows, aflows chan<- flow.Flow) (dialed, accepted *Conn, _ *wire) {
	dmrw, amrw, w := newMRWPair(ctx)
	versions := version.RPCVersionRange{Min: 3, Max: 5}
	ep, err := v23.NewEndpoint("localhost:80")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	d, err := NewDialed(ctx, dmrw, ep, ep, versions, fh(dflows), nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	a, err := NewAccepted(ctx, amrw, ep, security.Blessings{}, versions, fh(aflows))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	return d, a, w
}

func setupFlow(t *testing.T, ctx *context.T, dialFromDialer bool) (dialed flow.Flow, accepted <-chan flow.Flow) {
	dflows, aflows := make(chan flow.Flow, 1), make(chan flow.Flow, 1)
	d, a, _ := setupConns(t, ctx, dflows, aflows)
	if !dialFromDialer {
		d, a = a, d
		aflows, dflows = dflows, aflows
	}
	df, err := d.Dial(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	return df, aflows
}
