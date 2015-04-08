// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// fake implements a fake runtime.  The fake runtime is useful in tests when you
// want to mock out important components.
// TODO(mattr): Make a more complete, but still fake, implementation.
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
	return nil
}

func (r *Runtime) SetPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	return context.WithValue(ctx, principalKey, principal), nil
}

func (r *Runtime) GetPrincipal(ctx *context.T) security.Principal {
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (r *Runtime) GetAppCycle(ctx *context.T) v23.AppCycle {
	panic("unimplemented")
}

func (r *Runtime) SetBackgroundContext(ctx *context.T) *context.T {
	// Note we add an extra context with a nil value here.
	// This prevents users from travelling back through the
	// chain of background contexts.
	ctx = context.WithValue(ctx, backgroundKey, nil)
	return context.WithValue(ctx, backgroundKey, ctx)
}

func (r *Runtime) GetBackgroundContext(ctx *context.T) *context.T {
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

func (*Runtime) SetReservedNameDispatcher(ctx *context.T, d rpc.Dispatcher) *context.T {
	panic("unimplemented")
	return nil
}

func (*Runtime) GetReservedNameDispatcher(ctx *context.T) rpc.Dispatcher {
	panic("unimplmeneted")
	return nil
}
