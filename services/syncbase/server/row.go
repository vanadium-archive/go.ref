// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
)

// rowReq is a per-request object that handles Row RPCs.
type rowReq struct {
	key string
	c   *collectionReq
}

var (
	_ wire.RowServerMethods = (*rowReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (r *rowReq) Exists(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (bool, error) {
	allowExists := []access.Tag{access.Read, access.Write}

	impl := func(sntx store.SnapshotOrTransaction) (err error) {
		if _, err := common.GetPermsWithAuth(ctx, call, r.c, allowExists, sntx); err != nil {
			return err
		}
		return store.Get(ctx, sntx, r.stKey(), &vom.RawBytes{})
	}
	return common.ErrorToExists(r.c.d.runWithExistingBatchOrNewSnapshot(ctx, bh, impl))
}

func (r *rowReq) Get(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (*vom.RawBytes, error) {
	// Permissions checked in r.get.
	var res *vom.RawBytes
	impl := func(sntx store.SnapshotOrTransaction) (err error) {
		res, err = r.get(ctx, call, sntx)
		return err
	}
	if err := r.c.d.runWithExistingBatchOrNewSnapshot(ctx, bh, impl); err != nil {
		return nil, err
	}
	return res, nil
}

func (r *rowReq) Put(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, value *vom.RawBytes) error {
	// Permissions checked in r.put.
	impl := func(ts *transactionState) error {
		return r.put(ctx, call, ts, value)
	}
	return r.c.d.runInExistingBatchOrNewTransaction(ctx, bh, impl)
}

func (r *rowReq) Delete(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) error {
	// Permissions checked in r.delete.
	impl := func(ts *transactionState) error {
		return r.delete(ctx, call, ts)
	}
	return r.c.d.runInExistingBatchOrNewTransaction(ctx, bh, impl)
}

////////////////////////////////////////
// Internal helpers

func (r *rowReq) stKey() string {
	return common.JoinKeyParts(common.RowPrefix, r.stKeyPart())
}

func (r *rowReq) stKeyPart() string {
	return common.JoinKeyParts(r.c.stKeyPart(), r.key)
}

// get reads data from the storage engine.
// Performs authorization check.
func (r *rowReq) get(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction) (*vom.RawBytes, error) {
	allowGet := []access.Tag{access.Read}

	if _, err := common.GetPermsWithAuth(ctx, call, r.c, allowGet, sntx); err != nil {
		return nil, err
	}
	var valueAsRawBytes vom.RawBytes
	if err := store.Get(ctx, sntx, r.stKey(), &valueAsRawBytes); err != nil {
		return nil, err
	}
	return &valueAsRawBytes, nil
}

// put writes data to the storage engine.
// Performs authorization check.
func (r *rowReq) put(ctx *context.T, call rpc.ServerCall, ts *transactionState, value *vom.RawBytes) error {
	allowPut := []access.Tag{access.Write}

	currentPerms, err := common.GetPermsWithAuth(ctx, call, r.c, allowPut, ts.tx)
	if err != nil {
		return err
	}
	ts.MarkDataChanged(r.c.id, currentPerms)
	return store.Put(ctx, ts.tx, r.stKey(), value)
}

// delete deletes data from the storage engine.
// Performs authorization check.
func (r *rowReq) delete(ctx *context.T, call rpc.ServerCall, ts *transactionState) error {
	allowDelete := []access.Tag{access.Write}

	currentPerms, err := common.GetPermsWithAuth(ctx, call, r.c, allowDelete, ts.tx)
	if err != nil {
		return err
	}
	ts.MarkDataChanged(r.c.id, currentPerms)
	return store.Delete(ctx, ts.tx, r.stKey())
}
