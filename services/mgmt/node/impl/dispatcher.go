package impl

import (
	"sync"

	"veyron/services/mgmt/node"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/content"
)

// dispatcher holds the state of the node manager dispatcher.
type dispatcher struct {
	auth  security.Authorizer
	state *state
}

// crDispatcher holds the state of the node manager content repository
// dispatcher.
type crDispatcher struct {
	auth security.Authorizer
}

// arDispatcher holds the state of the node manager application
// repository dispatcher.
type arDispatcher struct {
	auth  security.Authorizer
	state *arState
}

// NewDispatcher is the node manager dispatcher factory.
func NewDispatchers(auth security.Authorizer, envelope *application.Envelope, name string) (*dispatcher, *crDispatcher, *arDispatcher) {
	return &dispatcher{
			auth: auth,
			state: &state{
				channels:      make(map[string]chan string),
				channelsMutex: new(sync.Mutex),
				envelope:      envelope,
				name:          name,
				updating:      false,
				updatingMutex: new(sync.Mutex),
			},
		}, &crDispatcher{
			auth: auth,
		}, &arDispatcher{
			auth: auth,
			state: &arState{
				envelope: envelope,
				name:     name,
			},
		}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(node.NewServerNode(NewInvoker(d.state, suffix))), d.auth, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *crDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(content.NewServerContent(NewCRInvoker(suffix))), d.auth, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *arDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(application.NewServerRepository(NewARInvoker(d.state, suffix))), d.auth, nil
}
