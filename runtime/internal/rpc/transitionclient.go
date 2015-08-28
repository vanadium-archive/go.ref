// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/flow/message"
	"v.io/v23/namespace"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/ref/runtime/internal/rpc/stream"
)

type transitionClient struct {
	c, xc rpc.Client
}

var _ = rpc.Client((*transitionClient)(nil))

func NewTransitionClient(ctx *context.T, streamMgr stream.Manager, ns namespace.T, opts ...rpc.ClientOpt) (rpc.Client, error) {
	var err error
	ret := &transitionClient{}
	// TODO(mattr): Un-comment this once servers are sending setups before closing
	// connections in error cases.
	// if ret.xc, err = InternalNewXClient(ctx, opts...); err != nil {
	// 	return nil, err
	// }
	if ret.c, err = InternalNewClient(streamMgr, ns, opts...); err != nil {
		ret.xc.Close()
		return nil, err
	}
	return ret, nil
}

func (t *transitionClient) StartCall(ctx *context.T, name, method string, args []interface{}, opts ...rpc.CallOpt) (rpc.ClientCall, error) {
	// The agent cannot reconnect, and it's never going to transition to the new
	// rpc system.  Instead it's moving off of rpc entirely.  For now we detect
	// and send it to the old rpc system.
	if t.xc == nil || strings.HasPrefix(name, "/@5@unixfd@") || strings.HasPrefix(name, "/@6@unixfd@") {
		return t.c.StartCall(ctx, name, method, args, opts...)
	}
	call, err := t.xc.StartCall(ctx, name, method, args, opts...)
	if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
		call, err = t.c.StartCall(ctx, name, method, args, opts...)
	}
	return call, err
}

func (t *transitionClient) Call(ctx *context.T, name, method string, in, out []interface{}, opts ...rpc.CallOpt) error {
	// The agent cannot reconnect, and it's never going to transition to the new
	// rpc system.  Instead it's moving off of rpc entirely.  For now we detect
	// and send it to the old rpc system.
	if t.xc == nil || strings.HasPrefix(name, "/@5@unixfd@") || strings.HasPrefix(name, "/@6@unixfd@") {
		return t.c.Call(ctx, name, method, in, out, opts...)
	}
	err := t.xc.Call(ctx, name, method, in, out, opts...)
	if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
		err = t.c.Call(ctx, name, method, in, out, opts...)
	}
	return err
}

func (t *transitionClient) Close() {
	if t.xc != nil {
		t.xc.Close()
	}
	t.c.Close()
}
