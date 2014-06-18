package impl

import (
	"sync"

	"veyron/services/mgmt/node"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/mgmt/application"
)

// dispatcher holds the state of the node manager dispatcher.
type dispatcher struct {
	auth  security.Authorizer
	state *state
}

// NewDispatcher is the node manager dispatcher factory.
func NewDispatcher(auth security.Authorizer, envelope *application.Envelope, name, previous string) *dispatcher {
	return &dispatcher{
		auth: auth,
		state: &state{
			channels:      make(map[string]chan string),
			channelsMutex: new(sync.Mutex),
			envelope:      envelope,
			name:          name,
			previous:      previous,
			updating:      false,
			updatingMutex: new(sync.Mutex),
		},
	}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(node.NewServerNode(NewInvoker(d.state, suffix))), d.auth, nil
}
