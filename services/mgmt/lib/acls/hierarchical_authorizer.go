package acls

import (
	"fmt"

	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"
)

// hierarchicalAuthorizer manages a pair of authorizers for two-level
// inheritance of ACLs.
type hierarchicalAuthorizer struct {
	root  security.Authorizer
	child security.Authorizer
}

// TAMGetter defines an abstract interface that a customer of
// NewHierarchicalAuthorizer can use to obtain the TaggedACLAuthorizer
// instances that it needs to construct a hierarchicalAuthorizer.
type TAMGetter interface {
	// TAMForPath has two successful outcomes: either returning a valid
	// TaggedACLMap object or a boolean status true indicating that the
	// TaggedACLMap object is intentionally not present. Finally, it returns an
	// error if anything has gone wrong.
	TAMForPath(path string) (access.TaggedACLMap, bool, error)
}

// NewHierarchicalAuthorizer creates a new hierarchicalAuthorizer
func NewHierarchicalAuthorizer(rootDir, childDir string, get TAMGetter) (security.Authorizer, error) {
	rootTam, intentionallyEmpty, err := get.TAMForPath(rootDir)
	if err != nil {
		return nil, err
	} else if intentionallyEmpty {
		vlog.VI(2).Infof("TAMForPath(%s) is intentionally empty", rootDir)
		return nil, nil
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
	childTam, intentionallyEmpty, err := get.TAMForPath(childDir)
	if err != nil {
		return nil, err
	} else if intentionallyEmpty {
		return rootAuth, nil
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
