// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/vdl"
	"v.io/v23/verror"
)

// TODO(sadovsky): Extend data layout to support version tracking for sync.
// See go/vanadium-local-structured-store.

// TODO(sadovsky): Handle the case where we're in a batch.

type row struct {
	key string
	t   *table
}

var (
	_ wire.RowServerMethods = (*row)(nil)
	_ util.Layer            = (*row)(nil)
)

////////////////////////////////////////
// RPC methods

func (r *row) Get(ctx *context.T, call rpc.ServerCall) (*vdl.Value, error) {
	return r.get(ctx, call, r.t.d.st)
}

func (r *row) Put(ctx *context.T, call rpc.ServerCall, value *vdl.Value) error {
	return r.put(ctx, call, r.t.d.st, value)
}

func (r *row) Delete(ctx *context.T, call rpc.ServerCall) error {
	return r.del(ctx, call, r.t.d.st)
}

////////////////////////////////////////
// util.Layer methods

func (r *row) Name() string {
	return r.key
}

func (r *row) StKey() string {
	return util.JoinKeyParts(util.RowPrefix, r.stKeyPart())
}

////////////////////////////////////////
// Internal helpers

func (r *row) stKeyPart() string {
	return util.JoinKeyParts(r.t.stKeyPart(), r.key)
}

// TODO(sadovsky): Update access checks to use prefix permissions.

// checkAccess checks that this row's table exists in the database and performs
// an authorization check (currently against the table perms).
// Returns a VDL-compatible error.
func (r *row) checkAccess(ctx *context.T, call rpc.ServerCall, st store.StoreReader) error {
	return util.Get(ctx, call, st, r.t.d, &databaseData{})
}

// get reads data from the storage engine.
// Performs authorization check.
// Returns a VDL-compatible error.
func (r *row) get(ctx *context.T, call rpc.ServerCall, st store.StoreReader) (*vdl.Value, error) {
	if err := r.checkAccess(ctx, call, st); err != nil {
		return nil, err
	}
	value := &vdl.Value{}
	if err := util.GetObject(st, r.StKey(), value); err != nil {
		if _, ok := err.(*store.ErrUnknownKey); ok {
			// We've already done an auth check, so here we can safely return NoExist
			// rather than NoExistOrNoAccess.
			return nil, verror.New(verror.ErrNoExist, ctx, r.Name())
		}
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return value, nil
}

// put writes data to the storage engine.
// Performs authorization check.
// Returns a VDL-compatible error.
func (r *row) put(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter, value *vdl.Value) error {
	if err := r.checkAccess(ctx, call, st); err != nil {
		return err
	}
	return util.Put(ctx, call, st, r, value)
}

// del deletes data from the storage engine.
// Performs authorization check.
// Returns a VDL-compatible error.
func (r *row) del(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter) error {
	if err := r.checkAccess(ctx, call, st); err != nil {
		return err
	}
	return util.Delete(ctx, call, st, r)
}
