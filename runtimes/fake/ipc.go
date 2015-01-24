package fake

import (
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
)

// SetClient can be used to inject a mock client implementation into the context.
func SetClient(ctx *context.T, client ipc.Client) *context.T {
	return context.WithValue(ctx, clientKey, client)
}
func (r *Runtime) SetNewClient(ctx *context.T, opts ...ipc.ClientOpt) (*context.T, ipc.Client, error) {
	panic("unimplemented")
}
func (r *Runtime) GetClient(ctx *context.T) ipc.Client {
	c, _ := ctx.Value(clientKey).(ipc.Client)
	return c
}

func (r *Runtime) NewServer(ctx *context.T, opts ...ipc.ServerOpt) (ipc.Server, error) {
	panic("unimplemented")
}
func (r *Runtime) SetNewStreamManager(ctx *context.T, opts ...stream.ManagerOpt) (*context.T, stream.Manager, error) {
	panic("unimplemented")
}
func (r *Runtime) GetStreamManager(ctx *context.T) stream.Manager {
	panic("unimplemented")
}

func (r *Runtime) GetListenSpec(ctx *context.T) ipc.ListenSpec {
	return ipc.ListenSpec{}
}
