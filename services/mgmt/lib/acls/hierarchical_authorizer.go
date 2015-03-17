package acls

import (
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"
)

// hierarchicalAuthorizer manages a pair of authorizers for two-level
// inheritance of AccessLists.
type hierarchicalAuthorizer struct {
	child          security.Authorizer
	rootAccessList access.AccessList
}

// TAMGetter defines an abstract interface that a customer of
// NewHierarchicalAuthorizer can use to obtain the PermissionsAuthorizer
// instances that it needs to construct a hierarchicalAuthorizer.
type TAMGetter interface {
	// TAMForPath has two successful outcomes: either returning a valid
	// Permissions object or a boolean status true indicating that the
	// Permissions object is intentionally not present. Finally, it returns an
	// error if anything has gone wrong.
	TAMForPath(path string) (access.Permissions, bool, error)
}

func mkRootAuth(rootTam access.Permissions) (security.Authorizer, error) {
	rootAuth, err := access.PermissionsAuthorizer(rootTam, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained an AccessList from the filesystem but PermissionsAuthorizer couldn't use it: %v", err)
		return nil, err
	}
	return rootAuth, nil
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

	// We are at the root so exit early.
	if rootDir == childDir {
		return mkRootAuth(rootTam)
	}

	// This is not fatal: the childDir may not exist if we are invoking
	// a Create() method so we only use the root AccessList.
	childTam, intentionallyEmpty, err := get.TAMForPath(childDir)
	if err != nil {
		return nil, err
	} else if intentionallyEmpty {
		return mkRootAuth(rootTam)
	}

	childAuth, err := access.PermissionsAuthorizer(childTam, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained an AccessList from the filesystem but PermissionsAuthorizer couldn't use it: %v", err)
		return nil, err
	}

	return &hierarchicalAuthorizer{
		child:          childAuth,
		rootAccessList: rootTam[string(access.Admin)],
	}, nil
}

// Authorize provides two-levels of authorization. Admin permission
// on the root provides a "superuser"-like power for administering the
// server using an instance of hierarchicalAuthorizer. Otherwise, the
// default permissions of the named path apply.
func (ha *hierarchicalAuthorizer) Authorize(ctx *context.T) error {
	childErr := ha.child.Authorize(ctx)
	if childErr == nil {
		return nil
	}

	// Maybe the invoking principal can invoke this method because
	// it has root permissions.
	names, _ := security.BlessingNames(ctx, security.CallSideRemote)
	if len(names) > 0 && ha.rootAccessList.Includes(names...) {
		return nil
	}

	return childErr
}
