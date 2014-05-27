package impl

import (
	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/node"
)

// dispatcher holds the state of the node manager dispatcher.
type dispatcher struct {
	envelope *application.Envelope
	origin   string
	auth     security.Authorizer
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(envelope *application.Envelope, origin string, auth security.Authorizer) *dispatcher {
	return &dispatcher{
		envelope: envelope,
		origin:   origin,
		auth:     auth,
	}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(node.NewServerNode(NewInvoker(d.envelope, d.origin, suffix)))
	return invoker, d.auth, nil
}
