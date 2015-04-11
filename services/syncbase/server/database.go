// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package server

import (
	"v.io/syncbase/v23/services/syncbase"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

// TODO(sadovsky): Decide whether to use the same version-aware data layout that
// we use for Items. Relatedly, decide whether the Database Permissions should get
// synced. (If so, that suggests we should indeed use the same version-aware
// data layout here, and perhaps everywhere.)

type database struct {
	name string
	u    *universe
}

var _ syncbase.DatabaseServerMethods = (*database)(nil)

func (d *database) Create(call rpc.ServerCall, acl access.Permissions) error {
	return store.RunInTransaction(d.u.s.st, func(st store.Store) error {
		// Update universeData.
		var uData *universeData
		if err := d.u.update(call, st, true, func(data *universeData) error {
			if _, ok := data.Databases[d.name]; ok {
				return verror.New(verror.ErrExist, call.Context(), d.name)
			}
			// https://github.com/veyron/release-issues/issues/1145
			if data.Databases == nil {
				data.Databases = map[string]struct{}{}
			}
			data.Databases[d.name] = struct{}{}
			uData = data
			return nil
		}); err != nil {
			return err
		}

		// Blind-write databaseData.
		if acl == nil {
			acl = uData.Permissions
		}
		data := &databaseData{
			Name:        d.name,
			Permissions: acl,
		}
		return d.put(call, st, data)
	})
}

func (d *database) Delete(call rpc.ServerCall) error {
	return store.RunInTransaction(d.u.s.st, func(st store.Store) error {
		// Read-check-delete databaseData.
		if _, err := d.get(call, st, true); err != nil {
			return err
		}
		// TODO(sadovsky): Delete all Tables (and Items) in this Database.
		if err := d.del(call, st); err != nil {
			return err
		}

		// Update universeData.
		return d.u.update(call, st, false, func(data *universeData) error {
			delete(data.Databases, d.name)
			return nil
		})
	})
}

func (d *database) UpdateSchema(call rpc.ServerCall, schema syncbase.Schema, version string) error {
	if schema == nil {
		return verror.New(verror.ErrInternal, nil, "schema must be specified")
	}

	return store.RunInTransaction(d.u.s.st, func(st store.Store) error {
		return d.update(call, st, true, func(data *databaseData) error {
			if err := checkVersion(call, version, data.Version); err != nil {
				return err
			}
			// TODO(sadovsky): Delete all Tables (and Items) that are no longer part
			// of the schema.
			data.Schema = schema
			data.Version++
			return nil
		})
	})
}

func (d *database) GetSchema(call rpc.ServerCall) (schema syncbase.Schema, version string, err error) {
	data, err := d.get(call, d.u.s.st, true)
	if err != nil {
		return nil, "", err
	}
	return data.Schema, formatVersion(data.Version), nil
}

func (d *database) SetPermissions(call rpc.ServerCall, acl access.Permissions, version string) error {
	return store.RunInTransaction(d.u.s.st, func(st store.Store) error {
		return d.update(call, st, true, func(data *databaseData) error {
			if err := checkVersion(call, version, data.Version); err != nil {
				return err
			}
			data.Permissions = acl
			data.Version++
			return nil
		})
	})
}

func (d *database) GetPermissions(call rpc.ServerCall) (acl access.Permissions, version string, err error) {
	data, err := d.get(call, d.u.s.st, true)
	if err != nil {
		return nil, "", err
	}
	return data.Permissions, formatVersion(data.Version), nil
}

// TODO(sadovsky): Implement Glob.

////////////////////////////////////////
// Internal helpers

func (d *database) keyPart() string {
	return joinKeyParts(d.u.keyPart(), d.name)
}

func (d *database) key() string {
	return joinKeyParts("$database", d.keyPart())
}

// Note, the methods below use "x" as the receiver name to make find-replace
// easier across the different levels of syncbase hierarchy.
//
// TODO(sadovsky): Is there any better way to share this code despite the lack
// of generics?

// Reads data from the storage engine.
// Returns a VDL-compatible error.
// checkAuth specifies whether to perform an authorization check.
func (x *database) get(call rpc.ServerCall, st store.Store, checkAuth bool) (*databaseData, error) {
	// TODO(kash): Get this to compile.
	return nil, nil
	// data := &databaseData{}
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
func (x *database) put(call rpc.ServerCall, st store.Store, data *databaseData) error {
	if err := putObject(st, x.key(), data); err != nil {
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

// Deletes data from the storage engine.
// Returns a VDL-compatible error.
// If you need to perform an authorization check, call get() first.
func (x *database) del(call rpc.ServerCall, st store.Store) error {
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
func (x *database) update(call rpc.ServerCall, st store.Store, checkAuth bool, fn func(data *databaseData) error) error {
	data, err := x.get(call, st, checkAuth)
	if err != nil {
		return err
	}
	if err := fn(data); err != nil {
		return err
	}
	return x.put(call, st, data)
}
