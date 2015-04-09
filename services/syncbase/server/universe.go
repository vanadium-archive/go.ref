// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/syncbase/v23/services/syncbase"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

type universe struct {
	name string
	s    *service
}

var _ syncbase.UniverseServerMethods = (*universe)(nil)

func (u *universe) Create(call rpc.ServerCall, acl access.Permissions) error {
	return store.RunInTransaction(u.s.st, func(st store.Store) error {
		// Update serviceData.
		var sData *serviceData
		if err := u.s.update(call, st, true, func(data *serviceData) error {
			if _, ok := data.Universes[u.name]; ok {
				return verror.New(verror.ErrExist, call.Context(), u.name)
			}
			// https://github.com/veyron/release-issues/issues/1145
			if data.Universes == nil {
				data.Universes = map[string]struct{}{}
			}
			data.Universes[u.name] = struct{}{}
			sData = data
			return nil
		}); err != nil {
			return err
		}

		// Blind-write universeData.
		if acl == nil {
			acl = sData.Permissions
		}
		data := &universeData{
			Name:        u.name,
			Permissions: acl,
		}
		return u.put(call, st, data)
	})
}

func (u *universe) Delete(call rpc.ServerCall) error {
	return store.RunInTransaction(u.s.st, func(st store.Store) error {
		// Read-check-delete universeData.
		if _, err := u.get(call, st, true); err != nil {
			return err
		}
		// TODO(sadovsky): Delete all Databases in this Universe.
		if err := u.del(call, st); err != nil {
			return err
		}

		// Update serviceData.
		return u.s.update(call, st, false, func(data *serviceData) error {
			delete(data.Universes, u.name)
			return nil
		})
	})
}

func (u *universe) SetPermissions(call rpc.ServerCall, acl access.Permissions, version string) error {
	return store.RunInTransaction(u.s.st, func(st store.Store) error {
		return u.update(call, st, true, func(data *universeData) error {
			if err := checkVersion(call, version, data.Version); err != nil {
				return err
			}
			data.Permissions = acl
			data.Version++
			return nil
		})
	})
}

func (u *universe) GetPermissions(call rpc.ServerCall) (acl access.Permissions, version string, err error) {
	data, err := u.get(call, u.s.st, true)
	if err != nil {
		return nil, "", err
	}
	return data.Permissions, formatVersion(data.Version), nil
}

// TODO(sadovsky): Implement Glob.

////////////////////////////////////////
// Internal helpers

func (u *universe) keyPart() string {
	return u.name
}

func (u *universe) key() string {
	return joinKeyParts("$universe", u.keyPart())
}

// Note, the methods below use "x" as the receiver name to make find-replace
// easier across the different levels of syncbase hierarchy.
//
// TODO(sadovsky): Is there any better way to share this code despite the lack
// of generics?

// Reads data from the storage engine.
// Returns a VDL-compatible error.
// checkAuth specifies whether to perform an authorization check.
func (x *universe) get(call rpc.ServerCall, st store.Store, checkAuth bool) (*universeData, error) {
	// TODO(kash): Get this to compile.
	return nil, nil
	// data := &universeData{}
	// if err := getObject(st, x.key(), data); err != nil {
	// 	if _, ok := err.(*store.ErrUnknownKey); ok {
	// 		// TODO(sadovsky): Return ErrNoExist if appropriate.
	// 		return nil, verror.NewErrNoExistOrNoAccess(call.Context())
	// 	}
	// 	return nil, verror.New(verror.ErrInternal, call.Context(), err)
	// }
	// if checkAuth {
	// 	auth, _ := access.PermissionsAuthorizer(data.Permissions, access.TypicalTagType())
	// 	if err := auth.Authorize(call); err != nil {
	// 		// TODO(sadovsky): Return ErrNoAccess if appropriate.
	// 		return nil, verror.NewErrNoExistOrNoAccess(call.Context())
	// 	}
	// }
	// return data, nil
}

// Writes data to the storage engine.
// Returns a VDL-compatible error.
// If you need to perform an authorization check, use update().
func (x *universe) put(call rpc.ServerCall, st store.Store, data *universeData) error {
	if err := putObject(st, x.key(), data); err != nil {
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

// Deletes data from the storage engine.
// Returns a VDL-compatible error.
// If you need to perform an authorization check, call get() first.
func (x *universe) del(call rpc.ServerCall, st store.Store) error {
	if err := st.Delete(x.key()); err != nil {
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

// Updates data in the storage engine.
// Returns a VDL-compatible error.
// checkAuth specifies whether to perform an authorization check.
// fn should perform the "modify" part of "read, modify, write", and should
// return a VDL-compatible error.
func (x *universe) update(call rpc.ServerCall, st store.Store, checkAuth bool, fn func(data *universeData) error) error {
	data, err := x.get(call, st, checkAuth)
	if err != nil {
		return err
	}
	if err := fn(data); err != nil {
		return err
	}
	return x.put(call, st, data)
}
