// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/lib/apilog"
)

// SetClient can be used to inject a mock client implementation into the context.
func SetClient(ctx *context.T, client rpc.Client) *context.T {
	return context.WithValue(ctx, clientKey, client)
}
func (r *Runtime) WithNewClient(ctx *context.T, opts ...rpc.ClientOpt) (*context.T, rpc.Client, error) {
	defer apilog.LogCallf(ctx, "opts...=%v", opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	panic("unimplemented")
}
func (r *Runtime) GetClient(ctx *context.T) rpc.Client {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	c, _ := ctx.Value(clientKey).(rpc.Client)
	return c
}

func (r *Runtime) NewServer(ctx *context.T, opts ...rpc.ServerOpt) (rpc.DeprecatedServer, error) {
	defer apilog.LogCallf(ctx, "opts...=%v", opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	panic("unimplemented")
}
func (r *Runtime) WithNewStreamManager(ctx *context.T) (*context.T, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	panic("unimplemented")
}

func (r *Runtime) GetListenSpec(ctx *context.T) rpc.ListenSpec {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	ls, _ := ctx.Value(listenSpecKey).(rpc.ListenSpec)
	return ls
}

func (r *Runtime) WithListenSpec(ctx *context.T, ls rpc.ListenSpec) *context.T {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return context.WithValue(ctx, listenSpecKey, ls)
	return ctx
}

func SetFlowManager(ctx *context.T, manager flow.Manager) *context.T {
	return context.WithValue(ctx, flowManagerKey, manager)
}

func (r *Runtime) ExperimentalGetFlowManager(ctx *context.T) flow.Manager {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	fm, _ := ctx.Value(flowManagerKey).(flow.Manager)
	return fm
}

func (r *Runtime) ExperimentalWithNewFlowManager(ctx *context.T) (*context.T, flow.Manager, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	panic("unimplemented")
}

func (r *Runtime) WithNewServer(ctx *context.T, name string, object interface{}, auth security.Authorizer, opts ...rpc.ServerOpt) (*context.T, rpc.Server, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	panic("unimplemented")
}

func (r *Runtime) WithNewDispatchingServer(ctx *context.T, name string, disp rpc.Dispatcher, opts ...rpc.ServerOpt) (*context.T, rpc.Server, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	panic("unimplemented")
}
