package acls

import (
	"fmt"
	"os"

	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"
)

// HierarchicalAuthorizer manages a pair of authorizers for two-level
// inheritance of ACLs.
type hierarchicalAuthorizer struct {
	root  security.Authorizer
	child security.Authorizer
}

// NewHierarchicalAuthorizer creates a new HierarchicalAuthorizer
func NewHierarchicalAuthorizer(principal security.Principal, rootDir, childDir string, locks *Locks) (security.Authorizer, error) {
	rootTam, _, err := locks.GetPathACL(rootDir)
	if os.IsNotExist(err) {
		vlog.VI(2).Infof("GetPathACL(%s) failed: %v, using default authorizer", rootDir, err)
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	rootAuth, err := access.TaggedACLAuthorizer(rootTam, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained an ACL from the filesystem but TaggedACLAuthorizer couldn't use it: %v", err)
		return nil, err
	}

	// We are at the root so exit early.
	if rootDir == childDir {
		return rootAuth, nil
	}

	// This is not fatal: the childDir may not exist if we are invoking
	// a Create() method so we only use the root ACL.
	childTam, _, err := locks.GetPathACL(childDir)
	if os.IsNotExist(err) {
		return rootAuth, nil
	} else if err != nil {
		return nil, err
	}

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

// Authorize provides two-levels of authorization. Permissions on "Roots"
// in the namespace will apply to children paths regardless of accesses
// set on the children. Conversely, ACL exclusions are not inherited.
func (ha *hierarchicalAuthorizer) Authorize(ctx security.Call) error {
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
