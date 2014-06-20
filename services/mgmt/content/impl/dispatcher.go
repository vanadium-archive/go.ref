package impl

import (
	"sync"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/mgmt/repository"
)

// dispatcher holds the state of the content repository dispatcher.
type dispatcher struct {
	root  string
	depth int
	fs    sync.Mutex
	auth  security.Authorizer
}

// newDispatcher is the dispatcher factory.
func NewDispatcher(root string, depth int, authorizer security.Authorizer) *dispatcher {
	return &dispatcher{root: root, auth: authorizer}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(repository.NewServerContent(newInvoker(d.root, d.depth, &d.fs, suffix)))
	return invoker, d.auth, nil
}
