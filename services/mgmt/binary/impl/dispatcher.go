package impl

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/repository"
)

const (
	VersionFile = "VERSION"
	Version     = "1.0"
)

// dispatcher holds the state of the binary repository dispatcher.
type dispatcher struct {
	auth  security.Authorizer
	state *state
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(state *state, authorizer security.Authorizer) ipc.Dispatcher {
	return &dispatcher{
		auth:  authorizer,
		state: state,
	}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	return repository.BinaryServer(newBinaryService(d.state, suffix)), d.auth, nil
}
