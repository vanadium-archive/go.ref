package fake

import (
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
)

func (r *Runtime) NewEndpoint(ep string) (naming.Endpoint, error) {
	panic("unimplemented")
}
func (r *Runtime) SetNewNamespace(ctx *context.T, roots ...string) (*context.T, ns.Namespace, error) {
	panic("unimplemented")
}
func (r *Runtime) GetNamespace(ctx *context.T) ns.Namespace {
	panic("unimplemented")
}
