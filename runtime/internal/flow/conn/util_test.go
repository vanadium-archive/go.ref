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
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/x/ref/internal/logger"
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
	logbuf := buf
	if len(buf) > 128 {
		logbuf = buf[:128]
	}
	logger.Global().VI(2).Infof("Writing %d bytes to the wire: %#v", len(buf), logbuf)
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
	logbuf := buf
	if len(buf) > 128 {
		logbuf = buf[:128]
	}
	logger.Global().VI(2).Infof("Reading %d bytes from the wire: %#v", len(buf), logbuf)
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

func setupConns(t *testing.T, dctx, actx *context.T, dflows, aflows chan<- flow.Flow) (dialed, accepted *Conn, _ *wire) {
	dmrw, amrw, w := newMRWPair(dctx)
	versions := version.RPCVersionRange{Min: 3, Max: 5}
	ep, err := v23.NewEndpoint("localhost:80")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	dch := make(chan *Conn)
	ach := make(chan *Conn)
	go func() {
		var handler FlowHandler
		if dflows != nil {
			handler = fh(dflows)
		}
		d, err := NewDialed(dctx, dmrw, ep, ep, versions, handler)
		if err != nil {
			panic(err)
		}
		dch <- d
	}()
	go func() {
		var handler FlowHandler
		if aflows != nil {
			handler = fh(aflows)
		}
		a, err := NewAccepted(actx, amrw, ep, versions, handler)
		if err != nil {
			panic(err)
		}
		ach <- a
	}()
	return <-dch, <-ach, w
}

func setupFlow(t *testing.T, dctx, actx *context.T, dialFromDialer bool) (dialed flow.Flow, accepted <-chan flow.Flow, close func()) {
	dflows, aflows := make(chan flow.Flow, 1), make(chan flow.Flow, 1)
	d, a, _ := setupConns(t, dctx, actx, dflows, aflows)
	if !dialFromDialer {
		d, a = a, d
		dctx, actx = actx, dctx
		aflows, dflows = dflows, aflows
	}
	df, err := d.Dial(dctx, testBFP)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	return df, aflows, func() { d.Close(dctx, nil); a.Close(actx, nil) }
}

func testBFP(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) (security.Blessings, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil
}

func makeBFP(in security.Blessings) flow.BlessingsForPeer {
	return func(
		ctx *context.T,
		localEndpoint, remoteEndpoint naming.Endpoint,
		remoteBlessings security.Blessings,
		remoteDischarges map[string]security.Discharge,
	) (security.Blessings, error) {
		return in, nil
	}
}
