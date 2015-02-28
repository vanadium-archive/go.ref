package impl

import (
	"path/filepath"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/core/veyron/security/flag"
	"v.io/core/veyron/services/mgmt/lib/fs"
	"v.io/core/veyron/services/mgmt/repository"
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

	acls, err := flag.TaggedACLMapFromFlag()
	if err != nil {
		return nil, err
	}
	if acls != nil {
		store.Lock()
		defer store.Unlock()

		// (Re)set the root ACLs.
		path := naming.Join("/acls", "data")
		_, tag, err := getACL(store, path)
		if err != nil && !verror.Is(err, ErrNotFound.ID) {
			return nil, err
		}
		if err := setACL(store, path, acls, tag); err != nil {
			return nil, err
		}
	}

	return &dispatcher{store: store, storeRoot: storeDir}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

// getAuthorizer searches the provided list of paths in the Memstore hierarchy
// for an TaggedACLMap and uses it to produce an authorizer or returns nil
// to get a nil Authorizer.
func getAuthorizer(store *fs.Memstore, paths []string) (security.Authorizer, error) {
	for _, p := range paths {
		if tam, _, err := getACL(store, p); err == nil {
			auth, err := access.TaggedACLAuthorizer(tam, access.TypicalTagType())
			if err != nil {
				vlog.Errorf("Successfully obtained an ACL from Memstore but TaggedACLAuthorizer couldn't use it: %v", err)
				return nil, err
			}
			return auth, nil
		} else if !verror.Is(err, ErrNotFound.ID) {
			vlog.Errorf("Internal error obtaining ACL from Memstore: %v", err)
		}
	}
	return nil, nil
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	app, version, _ := parse(nil, suffix)
	// TODO(rjkroege@google.com): Implement ACL inheritance.
	// Construct the search hierarchy for ACLs.
	sp := []string{
		naming.Join("/acls", app, version, "data"),
		naming.Join("/acls", app, "data"),
		naming.Join("/acls", "data"),
	}

	auth, err := getAuthorizer(d.store, sp)
	if err != nil {
		return nil, nil, err
	}
	return repository.ApplicationServer(NewApplicationService(d.store, d.storeRoot, suffix)), auth, nil
}
