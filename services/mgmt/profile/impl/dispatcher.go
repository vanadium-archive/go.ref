package impl

import (
	"veyron/services/mgmt/repository"

	"veyron2/ipc"
	"veyron2/security"
)

// dispatcher holds the state of the profile repository dispatcher.
type dispatcher struct {
	storeRoot string
	auth      security.Authorizer
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(name string, authorizer security.Authorizer) (*dispatcher, error) {
	return &dispatcher{storeRoot: name, auth: authorizer}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(repository.NewServerProfile(NewInvoker(d.storeRoot, suffix)))
	return invoker, d.auth, nil
}
