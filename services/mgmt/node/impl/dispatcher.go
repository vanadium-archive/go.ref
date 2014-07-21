package impl

import (
	"fmt"
	"sync"

	"veyron/services/mgmt/node"
	"veyron/services/mgmt/node/config"

	"veyron2/ipc"
	"veyron2/security"
)

// dispatcher holds the state of the node manager dispatcher.
type dispatcher struct {
	auth     security.Authorizer
	internal *internalState
	config   *config.State
}

// NewDispatcher is the node manager dispatcher factory.
func NewDispatcher(auth security.Authorizer, config *config.State) (*dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("Invalid config %v: %v", config, err)
	}

	return &dispatcher{
		auth: auth,
		internal: &internalState{
			channels:      make(map[string]chan string),
			channelsMutex: new(sync.Mutex),
			updating:      false,
			updatingMutex: new(sync.Mutex),
		},
		config: config,
	}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(node.NewServerNode(&invoker{
		internal: d.internal,
		config:   d.config,
		suffix:   suffix,
	})), d.auth, nil
}
