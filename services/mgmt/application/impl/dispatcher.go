package impl

import (
	"path/filepath"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"

	"v.io/x/ref/services/mgmt/lib/acls"
	"v.io/x/ref/services/mgmt/lib/fs"
	"v.io/x/ref/services/mgmt/repository"
)

// dispatcher holds the state of the application repository dispatcher.
type dispatcher struct {
	store     *fs.Memstore
	storeRoot string
}

// NewDispatcher is the dispatcher factory. storeDir is a path to a directory in which to
// serialize the applicationd state.
func NewDispatcher(storeDir string) (ipc.Dispatcher, error) {
	store, err := fs.NewMemstore(filepath.Join(storeDir, "applicationdstate.db"))
	if err != nil {
		return nil, err
	}
	return &dispatcher{store: store, storeRoot: storeDir}, nil
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	auth, err := acls.NewHierarchicalAuthorizer(
		naming.Join("/acls", "data"),
		naming.Join("/acls", suffix, "data"),
		(*applicationACLStore)(d.store))
	if err != nil {
		return nil, nil, err
	}
	return repository.ApplicationServer(NewApplicationService(d.store, d.storeRoot, suffix)), auth, nil
}

type applicationACLStore fs.Memstore

// TAMForPath implements TAMGetter so that applicationd can use the
// hierarchicalAuthorizer
func (store *applicationACLStore) TAMForPath(path string) (access.TaggedACLMap, bool, error) {
	tam, _, err := getACL((*fs.Memstore)(store), path)

	if verror.Is(err, ErrNotFound.ID) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return tam, false, nil
}
