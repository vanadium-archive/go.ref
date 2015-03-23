// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package acls

import (
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"

	"v.io/x/ref/profiles/internal/rpc"
)

// hierarchicalAuthorizer contains the state needed to implement
// hierarchical authorization in the Authorize method.
type hierarchicalAuthorizer struct {
	rootDir, childDir string
	get               TAMGetter
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

// NewHierarchicalAuthorizer creates a new hierarchicalAuthorizer: one
// that implements a "root" like concept: admin rights at the root of
// a server can invoke RPCs regardless of permissions set on child objects.
func NewHierarchicalAuthorizer(rootDir, childDir string, get TAMGetter) (security.Authorizer, error) {
	return &hierarchicalAuthorizer{
		rootDir:  rootDir,
		childDir: childDir,
		get:      get,
	}, nil
}

func (ha *hierarchicalAuthorizer) Authorize(ctx *context.T) error {
	rootPerms, intentionallyEmpty, err := ha.get.TAMForPath(ha.rootDir)
	if err != nil {
		return err
	} else if intentionallyEmpty {
		vlog.VI(2).Infof("TAMForPath(%s) is intentionally empty", ha.rootDir)
		return defaultAuthorizer(ctx)
	}

	// We are at the root so exit early.
	if ha.rootDir == ha.childDir {
		a, err := mkRootAuth(rootPerms)
		if err != nil {
			return err
		}
		return a.Authorize(ctx)
	}

	// This is not fatal: the childDir may not exist if we are invoking
	// a Create() method so we only use the root Permissions.
	childPerms, intentionallyEmpty, err := ha.get.TAMForPath(ha.childDir)
	if err != nil {
		return err
	} else if intentionallyEmpty {
		a, err := mkRootAuth(rootPerms)
		if err != nil {
			return err
		}
		return a.Authorize(ctx)
	}

	childAuth, err := access.PermissionsAuthorizer(childPerms, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained a Permissions from the filesystem but PermissionsAuthorizer couldn't use it: %v", err)
		return err
	}

	childErr := childAuth.Authorize(ctx)
	if childErr == nil {
		return nil
	}

	// Maybe the invoking principal can invoke this method because
	// it has root permissions.
	names, _ := security.RemoteBlessingNames(ctx)
	if len(names) > 0 && rootPerms[string(access.Admin)].Includes(names...) {
		return nil
	}

	return childErr
}

// defaultAuthorizer implements an authorization policy that requires one end
// of the RPC to have a blessing that makes it a delegate of the other.
// TODO(rjkroege): Remove this when the defaultAuthorizer becomes public.
func defaultAuthorizer(ctx *context.T) error {
	var (
		localNames             = security.LocalBlessingNames(ctx)
		remoteNames, remoteErr = security.RemoteBlessingNames(ctx)
	)
	// Authorize if any element in localNames is a "delegate of" (i.e., has been
	// blessed by) any element in remoteNames, OR vice-versa.
	for _, l := range localNames {
		if security.BlessingPattern(l).MatchedBy(remoteNames...) {
			// l is a delegate of an element in remote.
			return nil
		}
	}
	for _, r := range remoteNames {
		if security.BlessingPattern(r).MatchedBy(localNames...) {
			// r is a delegate of an element in localNames.
			return nil
		}
	}

	return rpc.NewErrInvalidBlessings(nil, remoteNames, remoteErr, localNames)
}
