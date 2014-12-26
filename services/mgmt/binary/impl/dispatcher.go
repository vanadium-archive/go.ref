package impl

import (
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/repository"
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

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.BinaryServer(newBinaryService(d.state, suffix)), d.auth, nil
}
