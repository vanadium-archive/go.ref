package impl

import (
	"veyron/services/mgmt/application"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/storage/vstore"
)

// dispatcher holds the state of the application manager dispatcher.
type dispatcher struct {
	store storage.Store
	auth  security.Authorizer
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(name string, authorizer security.Authorizer) (*dispatcher, error) {
	store, err := vstore.New(name)
	if err != nil {
		return nil, err
	}
	return &dispatcher{store: store, auth: authorizer}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(application.NewServerRepository(NewInvoker(d.store, suffix)))
	return invoker, d.auth, nil
}
