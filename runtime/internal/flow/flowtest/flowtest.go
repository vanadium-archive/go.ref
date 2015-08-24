// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flowtest

import (
	"io"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"

	"v.io/x/ref/internal/logger"
)

type Wire struct {
	ctx    *context.T
	mu     sync.Mutex
	c      *sync.Cond
	closed bool
}

func (w *Wire) Close() {
	w.mu.Lock()
	w.closed = true
	w.c.Broadcast()
	w.mu.Unlock()
}

func (w *Wire) IsClosed() bool {
	w.mu.Lock()
	c := w.closed
	w.mu.Unlock()
	return c
}

type MRW struct {
	wire *Wire
	in   []byte
	peer *MRW
}

func NewMRWPair(ctx *context.T) (*MRW, *MRW, *Wire) {
	w := &Wire{ctx: ctx}
	w.c = sync.NewCond(&w.mu)
	a, b := &MRW{wire: w}, &MRW{wire: w}
	a.peer, b.peer = b, a
	return a, b, w
}

func (f *MRW) WriteMsg(data ...[]byte) (int, error) {
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

func (f *MRW) ReadMsg() (buf []byte, err error) {
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

func (f *MRW) Close() error {
	f.wire.Close()
	return nil
}

func BlessingsForPeer(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) (security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}
