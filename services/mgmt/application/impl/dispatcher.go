package impl

import (
	"veyron.io/veyron/veyron/services/mgmt/repository"

	"veyron.io/veyron/veyron/services/mgmt/lib/fs"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
)

// dispatcher holds the state of the application repository dispatcher.
type dispatcher struct {
	store     *fs.Memstore
	auth      security.Authorizer
	storeRoot string
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

// NewDispatcher is the dispatcher factory.
func NewDispatcher(name string, authorizer security.Authorizer) (*dispatcher, error) {
	// TODO(rjkroege@google.com): Use the config service.
	store, err := fs.NewMemstore("")
	if err != nil {
		return nil, err
	}
	return &dispatcher{store: store, storeRoot: name, auth: authorizer}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.ApplicationServer(NewApplicationService(d.store, d.storeRoot, suffix)), d.auth, nil
}
