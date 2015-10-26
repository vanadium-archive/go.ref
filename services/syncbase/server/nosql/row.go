// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
)

// rowReq is a per-request object that handles Row RPCs.
type rowReq struct {
	key string
	t   *tableReq
}

var (
	_ wire.RowServerMethods = (*rowReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (r *rowReq) Exists(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (bool, error) {
	_, err := r.Get(ctx, call, schemaVersion)
	return util.ErrorToExists(err)
}

func (r *rowReq) Get(ctx *context.T, call rpc.ServerCall, schemaVersion int32) ([]byte, error) {
	impl := func(sntx store.SnapshotOrTransaction) ([]byte, error) {
		if err := r.t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return []byte{}, err
		}
		return r.get(ctx, call, sntx)
	}
	if r.t.d.batchId != nil {
		return impl(r.t.d.batchReader())
	} else {
		sn := r.t.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

func (r *rowReq) Put(ctx *context.T, call rpc.ServerCall, schemaVersion int32, value []byte) error {
	impl := func(tx store.Transaction) error {
		if err := r.t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		return r.put(ctx, call, tx, value)
	}
	if r.t.d.batchId != nil {
		if tx, err := r.t.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(r.t.d.st, impl)
	}
}

func (r *rowReq) Delete(ctx *context.T, call rpc.ServerCall, schemaVersion int32) error {
	impl := func(tx store.Transaction) error {
		if err := r.t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		return r.delete(ctx, call, tx)
	}
	if r.t.d.batchId != nil {
		if tx, err := r.t.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(r.t.d.st, impl)
	}
}

////////////////////////////////////////
// Internal helpers

func (r *rowReq) stKey() string {
	return util.JoinKeyParts(util.RowPrefix, r.stKeyPart())
}

func (r *rowReq) stKeyPart() string {
	return util.JoinKeyParts(r.t.stKeyPart(), r.key)
}

// checkAccess checks that this row's table exists in the database, and performs
// an authorization check.
// checkAccess returns the longest prefix of the given key that has associated
// permissions if the access is granted.
func (r *rowReq) checkAccess(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction) (string, error) {
	return r.t.checkAccess(ctx, call, sntx, r.key)
}

// get reads data from the storage engine.
// Performs authorization check.
func (r *rowReq) get(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction) ([]byte, error) {
	if _, err := r.checkAccess(ctx, call, sntx); err != nil {
		return nil, err
	}
	value, err := sntx.Get([]byte(r.stKey()), nil)
	if err != nil {
		if verror.ErrorID(err) == store.ErrUnknownKey.ID {
			return nil, verror.New(verror.ErrNoExist, ctx, r.stKey())
		}
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return value, nil
}

// put writes data to the storage engine.
// Performs authorization check.
func (r *rowReq) put(ctx *context.T, call rpc.ServerCall, tx store.Transaction, value []byte) error {
	permsPrefix, err := r.checkAccess(ctx, call, tx)
	if err != nil {
		return err
	}
	// TODO(rogulenko): Avoid the redundant lookups since in theory we have all
	// we need from the checkAccess.
	permsKey := r.t.prefixPermsKey(permsPrefix)
	if err := watchable.PutWithPerms(tx, []byte(r.stKey()), value, permsKey); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// delete deletes data from the storage engine.
// Performs authorization check.
func (r *rowReq) delete(ctx *context.T, call rpc.ServerCall, tx store.Transaction) error {
	permsPrefix, err := r.checkAccess(ctx, call, tx)
	if err != nil {
		return err
	}
	// TODO(rogulenko): Avoid the redundant lookups since in theory we have all
	// we need from the checkAccess.
	permsKey := r.t.prefixPermsKey(permsPrefix)
	if err := watchable.DeleteWithPerms(tx, []byte(r.stKey()), permsKey); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}
