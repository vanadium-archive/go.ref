package impl

import (
	"path/filepath"

	"v.io/v23/ipc"
	"v.io/v23/security"

	"v.io/x/ref/services/mgmt/lib/fs"
	"v.io/x/ref/services/mgmt/repository"
)

// dispatcher holds the state of the profile repository dispatcher.
type dispatcher struct {
	store     *fs.Memstore
	auth      security.Authorizer
	storeRoot string
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

// NewDispatcher is the dispatcher factory. storeDir is a path to a
// directory in which the profile state is persisted.
func NewDispatcher(storeDir string, authorizer security.Authorizer) (ipc.Dispatcher, error) {
	store, err := fs.NewMemstore(filepath.Join(storeDir, "profilestate.db"))
	if err != nil {
		return nil, err
	}
	return &dispatcher{store: store, storeRoot: storeDir, auth: authorizer}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return repository.ProfileServer(NewProfileService(d.store, d.storeRoot, suffix)), d.auth, nil
}
