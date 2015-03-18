package fake

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
)

// SetClient can be used to inject a mock client implementation into the context.
func SetClient(ctx *context.T, client rpc.Client) *context.T {
	return context.WithValue(ctx, clientKey, client)
}
func (r *Runtime) SetNewClient(ctx *context.T, opts ...rpc.ClientOpt) (*context.T, rpc.Client, error) {
	panic("unimplemented")
}
func (r *Runtime) GetClient(ctx *context.T) rpc.Client {
	c, _ := ctx.Value(clientKey).(rpc.Client)
	return c
}

func (r *Runtime) NewServer(ctx *context.T, opts ...rpc.ServerOpt) (rpc.Server, error) {
	panic("unimplemented")
}
func (r *Runtime) SetNewStreamManager(ctx *context.T) (*context.T, error) {
	panic("unimplemented")
}

func (r *Runtime) GetListenSpec(ctx *context.T) rpc.ListenSpec {
	return rpc.ListenSpec{}
}
