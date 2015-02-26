package impl

import (
	"path/filepath"

	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/repository"

	"v.io/core/veyron/services/mgmt/lib/acls"
)

const (
	VersionFile = "VERSION"
	Version     = "1.0"
)

// dispatcher holds the state of the binary repository dispatcher.
type dispatcher struct {
	state     *state
	locks     *acls.Locks
	principal security.Principal
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(principal security.Principal, state *state) (ipc.Dispatcher, error) {
	return &dispatcher{
		state: state,
		locks: acls.NewLocks(principal),
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

func newAuthorizer(principal security.Principal, rootDir, suffix string, locks *acls.Locks) (security.Authorizer, error) {
	return acls.NewHierarchicalAuthorizer(principal,
		aclPath(rootDir, ""),
		aclPath(rootDir, suffix),
		locks)
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	auth, err := newAuthorizer(d.principal, d.state.rootDir, suffix, d.locks)
	if err != nil {
		return nil, nil, err
	}
	return repository.BinaryServer(newBinaryService(d.state, suffix, d.locks)), auth, nil
}
