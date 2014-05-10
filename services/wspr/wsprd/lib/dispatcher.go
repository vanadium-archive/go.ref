package lib

import (
	"veyron2/ipc"
	"veyron2/security"
)

// dispatcher holds the invoker and the authorizer to be used for lookup.
type dispatcher struct {
	invoker    ipc.Invoker
	authorizer security.Authorizer
}

// newDispatcher is a dispatcher factory.
func newDispatcher(invoker ipc.Invoker, authorizer security.Authorizer) *dispatcher {
	return &dispatcher{invoker, authorizer}
}

// Lookup implements dispatcher interface Lookup.
func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return d.invoker, d.authorizer, nil
}

// StopServing implements dispatcher StopServing.
func (*dispatcher) StopServing() {
}
