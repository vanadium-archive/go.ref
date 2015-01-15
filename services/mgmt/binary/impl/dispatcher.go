package impl

import (
	"fmt"
	"os"
	"path/filepath"

	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/repository"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vlog"

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
func NewDispatcher(principal security.Principal, state *state) (*dispatcher, error) {
	return &dispatcher{
		state:     state,
		locks:     acls.NewLocks(),
		principal: principal,
	}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

type hierarchicalAuthorizer struct {
	root  security.Authorizer
	child security.Authorizer
}

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
	aclDir := aclPath(rootDir, "")
	rootTam, _, err := locks.GetPathACL(principal, aclDir)
	if err != nil && os.IsNotExist(err) {
		vlog.VI(2).Infof("GetPathACL(%s) failed: %v, using default authorizer", aclDir, err)
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	rootAuth, err := access.TaggedACLAuthorizer(rootTam, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained an ACL from the filesystem but TaggedACLAuthorizer couldn't use it: %v", err)
		return nil, err
	}

	if suffix == "" {
		return rootAuth, nil
	}

	// This is not fatal: the suffix may not exist if we are invoking
	// a Create() method so we only use the root ACL.
	aclDir = aclPath(rootDir, suffix)
	childTam, _, err := locks.GetPathACL(principal, aclDir)
	if err != nil && os.IsNotExist(err) {
		return rootAuth, nil
	} else if err != nil {
		return nil, err
	}
	// Root's blacklist also applies to the child.
	meldBlacklists(rootTam, childTam)

	childAuth, err := access.TaggedACLAuthorizer(childTam, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained an ACL from the filesystem but TaggedACLAuthorizer couldn't use it: %v", err)
		return nil, err
	}

	return &hierarchicalAuthorizer{
		root:  rootAuth,
		child: childAuth,
	}, nil
}

func meldBlacklists(root, child access.TaggedACLMap) {
	for n, r := range root {
		for _, b := range r.NotIn {
			child.Blacklist(b, n)
		}
	}
}

// Authorize implements a chain of logic that works like this: If the
// root blacklists or the child blacklists, (as implemented by
// meldBlacklists) then the remote principal is forbidden to connect.
// This way, the administrator can blacklist a principal even if the
// child principal has admin permissions on the suffix. Otherwise, Authorize first
// checks the child authorizer and then fallbacks to the root authorizer.
// TODO(rjkroege): Introduce a different label for Create().
func (ha *hierarchicalAuthorizer) Authorize(ctx security.Context) error {
	childErr := ha.child.Authorize(ctx)
	if childErr == nil {
		return nil
	}
	rootErr := ha.root.Authorize(ctx)
	if rootErr == nil {
		return nil
	}
	return fmt.Errorf("Both root acls (%v) and child acls (%v) deny access.", rootErr, childErr)
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	auth, err := newAuthorizer(d.principal, d.state.rootDir, suffix, d.locks)
	if err != nil {
		return nil, nil, err
	}
	return repository.BinaryServer(newBinaryService(d.state, suffix, d.locks)), auth, nil
}
