// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package server

import (
	"v.io/syncbase/v23/services/syncbase"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/rpc"
	"v.io/v23/vdl"
	"v.io/v23/verror"
)

// TODO(sadovsky): Extend data layout to support version tracking for sync.
// See go/vanadium-local-structured-store.

type item struct {
	encodedKey string
	t          *table
}

var _ syncbase.ItemServerMethods = (*item)(nil)

func (i *item) Get(call rpc.ServerCall) (*vdl.Value, error) {
	var value *vdl.Value
	if err := store.RunInTransaction(i.t.d.u.s.st, func(st store.Store) error {
		var err error
		value, err = i.get(call, st)
		return err
	}); err != nil {
		return nil, err
	}
	return value, nil
}

func (i *item) Put(call rpc.ServerCall, value *vdl.Value) error {
	return store.RunInTransaction(i.t.d.u.s.st, func(st store.Store) error {
		return i.put(call, st, value)
	})
}

func (i *item) Delete(call rpc.ServerCall) error {
	return store.RunInTransaction(i.t.d.u.s.st, func(st store.Store) error {
		return i.del(call, st)
	})
}

////////////////////////////////////////
// Internal helpers

func (i *item) keyPart() string {
	return joinKeyParts(i.t.keyPart(), i.encodedKey)
}

func (i *item) key() string {
	return joinKeyParts("$item", i.keyPart())
}

// Performs authorization check (against the database Permissions) and checks that this
// item's table exists in the database schema. Returns the schema and a
// VDL-compatible error.
func (i *item) checkAccess(call rpc.ServerCall, st store.Store) (syncbase.Schema, error) {
	data, err := i.t.d.get(call, st, true)
	if err != nil {
		return nil, err
	}
	if _, ok := data.Schema[i.t.name]; !ok {
		return nil, verror.NewErrNoExist(call.Context())
	}
	return data.Schema, nil
}

// Reads data from the storage engine.
// Performs authorization check. Returns a VDL-compatible error.
func (i *item) get(call rpc.ServerCall, st store.Store) (*vdl.Value, error) {
	if _, err := i.checkAccess(call, st); err != nil {
		return nil, err
	}
	value := &vdl.Value{}
	if err := getObject(st, i.key(), value); err != nil {
		if _, ok := err.(*store.ErrUnknownKey); ok {
			// We've already done an auth check, so here we can safely return NoExist
			// rather than NoExistOrNoAccess.
			return nil, verror.NewErrNoExist(call.Context())
		}
		return nil, verror.New(verror.ErrInternal, call.Context(), err)
	}
	return value, nil
}

// Writes data to the storage engine.
// Performs authorization check. Returns a VDL-compatible error.
func (i *item) put(call rpc.ServerCall, st store.Store, value *vdl.Value) error {
	// TODO(sadovsky): Check that value's primary key field matches i.encodedKey.
	if _, err := i.checkAccess(call, st); err != nil {
		return err
	}
	if err := putObject(st, i.key(), &value); err != nil {
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

// Deletes data from the storage engine.
// Performs authorization check. Returns a VDL-compatible error.
func (i *item) del(call rpc.ServerCall, st store.Store) error {
	if _, err := i.checkAccess(call, st); err != nil {
		return err
	}
	if err := st.Delete(i.key()); err != nil {
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}
