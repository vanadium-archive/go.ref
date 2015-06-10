// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"

	vsecurity "v.io/x/ref/lib/security"
)

type contextKey int

const (
	clientKey = contextKey(iota)
	principalKey
	loggerKey
	backgroundKey
)

type Runtime struct{}

func new(ctx *context.T) (*Runtime, *context.T, v23.Shutdown, error) {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		return nil, nil, func() {}, err
	}
	ctx = context.WithValue(ctx, principalKey, p)
	return &Runtime{}, ctx, func() {}, nil
}

func (r *Runtime) Init(ctx *context.T) error {
	// nologcall
	return nil
}

func (r *Runtime) WithPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	// nologcall
	return context.WithValue(ctx, principalKey, principal), nil
}

func (r *Runtime) GetPrincipal(ctx *context.T) security.Principal {
	// nologcall
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (r *Runtime) GetAppCycle(ctx *context.T) v23.AppCycle {
	// nologcall
	panic("unimplemented")
}

func (r *Runtime) WithBackgroundContext(ctx *context.T) *context.T {
	// nologcall
	// Note we add an extra context with a nil value here.
	// This prevents users from travelling back through the
	// chain of background contexts.
	ctx = context.WithValue(ctx, backgroundKey, nil)
	return context.WithValue(ctx, backgroundKey, ctx)
}

func (r *Runtime) GetBackgroundContext(ctx *context.T) *context.T {
	// nologcall
	bctx, _ := ctx.Value(backgroundKey).(*context.T)
	if bctx == nil {
		// There should always be a background context.  If we don't find
		// it, that means that the user passed us the background context
		// in hopes of following the chain.  Instead we just give them
		// back what they sent in, which is correct.
		return ctx
	}
	return bctx
}

func (*Runtime) WithReservedNameDispatcher(ctx *context.T, d rpc.Dispatcher) *context.T {
	// nologcall
	panic("unimplemented")
}

func (*Runtime) GetReservedNameDispatcher(ctx *context.T) rpc.Dispatcher {
	// nologcall
	panic("unimplmeneted")
}
