// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"v.io/v23/context"
	"v.io/v23/flow/message"
	"v.io/v23/namespace"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/ref/runtime/internal/rpc/stream"
)

type transitionClient struct {
	c, xc  rpc.Client
	ctx    *context.T
	cancel context.CancelFunc
}

type connTest interface {
	Connected(ctx *context.T, name string, opts ...rpc.CallOpt) bool
}

var _ = rpc.Client((*transitionClient)(nil))

func NewTransitionClient(ctx *context.T, streamMgr stream.Manager, ns namespace.T, opts ...rpc.ClientOpt) rpc.Client {
	// The transition client behaves like the old RPC client.  It
	// doesn't die until Close is called.  This is important because
	// unlike the new RPC server, the old one doesn't control the
	// lifetime of the client it uses.  If the client closes when the
	// context closes, then the server will not be able to unmount
	// cleanly.
	rootCtx, rootCancel := context.WithRootCancel(ctx)
	tc := &transitionClient{
		xc:     NewXClient(rootCtx, ns, opts...),
		c:      DeprecatedNewClient(rootCtx, streamMgr, ns, opts...),
		ctx:    rootCtx,
		cancel: rootCancel,
	}
	return tc
}

func (t *transitionClient) StartCall(ctx *context.T, name, method string, args []interface{}, opts ...rpc.CallOpt) (rpc.ClientCall, error) {
	if t.c.(connTest).Connected(ctx, name, opts...) {
		return t.c.StartCall(ctx, name, method, args, opts...)
	}
	call, err := t.xc.StartCall(ctx, name, method, args, opts...)
	if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
		call, err = t.c.StartCall(ctx, name, method, args, opts...)
	}
	return call, err
}

func (t *transitionClient) Call(ctx *context.T, name, method string, in, out []interface{}, opts ...rpc.CallOpt) error {
	if t.c.(connTest).Connected(ctx, name, opts...) {
		return t.c.Call(ctx, name, method, in, out, opts...)
	}
	err := t.xc.Call(ctx, name, method, in, out, opts...)
	if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
		err = t.c.Call(ctx, name, method, in, out, opts...)
	}
	return err
}

func (t *transitionClient) Close() {
	t.c.Close()
	t.xc.Close()
	if t.cancel != nil {
		t.cancel()
	}
}

func (t *transitionClient) Closed() <-chan struct{} {
	return t.ctx.Done()
}
