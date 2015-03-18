package impl

import (
	"path/filepath"

	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/repository"

	"v.io/x/ref/services/mgmt/lib/acls"
)

const (
	VersionFile = "VERSION"
	Version     = "1.0"
)

// dispatcher holds the state of the binary repository dispatcher.
type dispatcher struct {
	state    *state
	aclstore *acls.PathStore
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(principal security.Principal, state *state) (rpc.Dispatcher, error) {
	return &dispatcher{
		state:    state,
		aclstore: acls.NewPathStore(principal),
	}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func aclPath(rootDir, suffix string) string {
	var dir string
	if suffix == "" {
		// Directory is in namespace overlapped with Vanadium namespace
		// so hide it.
		dir = filepath.Join(rootDir, "__acls")
	} else {
		dir = filepath.Join(rootDir, suffix, "acls")
	}
	return dir
}

func newAuthorizer(rootDir, suffix string, aclstore *acls.PathStore) (security.Authorizer, error) {
	return acls.NewHierarchicalAuthorizer(
		aclPath(rootDir, ""),
		aclPath(rootDir, suffix),
		aclstore)
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	auth, err := newAuthorizer(d.state.rootDir, suffix, d.aclstore)
	if err != nil {
		return nil, nil, err
	}
	return repository.BinaryServer(newBinaryService(d.state, suffix, d.aclstore)), auth, nil
}
