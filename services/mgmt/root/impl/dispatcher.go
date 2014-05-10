package impl

import (
	"veyron/services/mgmt/root"
	"veyron2/ipc"
	"veyron2/security"
)

// dispatcher holds the state of the root process.
type dispatcher struct {
	state *invoker
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher() *dispatcher {
	return &dispatcher{NewInvoker()}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(root.NewServerRoot(d.state)), nil, nil
}
