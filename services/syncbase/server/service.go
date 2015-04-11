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

type service struct {
	st store.TransactableStore
}

var _ syncbase.ServiceServerMethods = (*service)(nil)

func NewService(st store.TransactableStore) *service {
	return &service{st: st}
}

// Returns a VDL-compatible error.
// Note, this is not an RPC method.
func (s *service) Create(acl access.Permissions) error {
	if acl == nil {
		return verror.New(verror.ErrInternal, nil, "acl must be specified")
	}

	return store.RunInTransaction(s.st, func(st store.Store) error {
		// TODO(sadovsky): Maybe add "has" method to storage engine.
		data := &serviceData{}
		if err := getObject(st, s.key(), data); err == nil {
			return verror.NewErrExist(nil)
		}

		data = &serviceData{
			Permissions: acl,
		}
		if err := putObject(st, s.key(), data); err != nil {
			return verror.New(verror.ErrInternal, nil, "put failed")
		}

		return nil
	})
}

func (s *service) SetPermissions(call rpc.ServerCall, acl access.Permissions, version string) error {
	return store.RunInTransaction(s.st, func(st store.Store) error {
		return s.update(call, st, true, func(data *serviceData) error {
			if err := checkVersion(call, version, data.Version); err != nil {
				return err
			}
			data.Permissions = acl
			data.Version++
			return nil
		})
	})
}

func (s *service) GetPermissions(call rpc.ServerCall) (acl access.Permissions, version string, err error) {
	data, err := s.get(call, s.st, true)
	if err != nil {
		return nil, "", err
	}
	return data.Permissions, formatVersion(data.Version), nil
}

// TODO(sadovsky): Implement Glob.

////////////////////////////////////////
// Internal helpers

func (s *service) key() string {
	return "$service"
}

// Note, the methods below use "x" as the receiver name to make find-replace
// easier across the different levels of syncbase hierarchy.
//
// TODO(sadovsky): Is there any better way to share this code despite the lack
// of generics?

// Reads data from the storage engine.
// Returns a VDL-compatible error.
// checkAuth specifies whether to perform an authorization check.
func (x *service) get(call rpc.ServerCall, st store.Store, checkAuth bool) (*serviceData, error) {
	// TODO(kash): Get this to compile.
	return nil, nil
	// data := &serviceData{}
	// if err := getObject(st, x.key(), data); err != nil {
	// 	if _, ok := err.(*store.ErrUnknownKey); ok {
	// 		// TODO(sadovsky): Return ErrNoExist if appropriate.
	// 		return nil, verror.New(verror.ErrNoExistOrNoAccess, call.Context())
	// 	}
	// 	return nil, verror.New(verror.ErrInternal, call.Context(), err)
	// }
	// if checkAuth {
	// 	auth, _ := access.PermissionsAuthorizer(data.Permissions, access.TypicalTagType())
	// 	if err := auth.Authorize(call); err != nil {
	// 		// TODO(sadovsky): Return ErrNoAccess if appropriate.
	// 		return nil, verror.New(verror.ErrNoExistOrNoAccess, call.Context(), err)
	// 	}
	// }
	// return data, nil
}

// Writes data to the storage engine.
// Returns a VDL-compatible error.
// If you need to perform an authorization check, use update().
func (x *service) put(call rpc.ServerCall, st store.Store, data *serviceData) error {
	if err := putObject(st, x.key(), data); err != nil {
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

// Deletes data from the storage engine.
// Returns a VDL-compatible error.
// If you need to perform an authorization check, call get() first.
func (x *service) del(call rpc.ServerCall, st store.Store) error {
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
func (x *service) update(call rpc.ServerCall, st store.Store, checkAuth bool, fn func(data *serviceData) error) error {
	data, err := x.get(call, st, checkAuth)
	if err != nil {
		return err
	}
	if err := fn(data); err != nil {
		return err
	}
	return x.put(call, st, data)
}
