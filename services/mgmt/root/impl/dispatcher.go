package impl

import (
	"veyron.io/veyron/veyron/services/mgmt/root"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
)

// dispatcher holds the state of the root process.
type dispatcher struct {
	state *invoker
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

// NewDispatcher is the dispatcher factory.
func NewDispatcher() *dispatcher {
	return &dispatcher{NewInvoker()}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	return root.RootServer(d.state), nil, nil
}
