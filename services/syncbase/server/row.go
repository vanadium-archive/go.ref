// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/watchable"
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

func (r *rowReq) Exists(ctx *context.T, call rpc.ServerCall) (bool, error) {
	_, err := r.Get(ctx, call)
	return util.ErrorToExists(err)
}

func (r *rowReq) Get(ctx *context.T, call rpc.ServerCall) ([]byte, error) {
	impl := func(sntx store.SnapshotOrTransaction) ([]byte, error) {
		return r.get(ctx, call, sntx)
	}
	if r.c.d.batchId != nil {
		return impl(r.c.d.batchReader())
	} else {
		sn := r.c.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

func (r *rowReq) Put(ctx *context.T, call rpc.ServerCall, value []byte) error {
	impl := func(tx *watchable.Transaction) error {
		return r.put(ctx, call, tx, value)
	}
	if r.c.d.batchId != nil {
		if tx, err := r.c.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return watchable.RunInTransaction(r.c.d.st, impl)
	}
}

func (r *rowReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	impl := func(tx *watchable.Transaction) error {
		return r.delete(ctx, call, tx)
	}
	if r.c.d.batchId != nil {
		if tx, err := r.c.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return watchable.RunInTransaction(r.c.d.st, impl)
	}
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
func (r *rowReq) get(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction) ([]byte, error) {
	if err := r.c.checkAccess(ctx, call, sntx); err != nil {
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
func (r *rowReq) put(ctx *context.T, call rpc.ServerCall, tx *watchable.Transaction, value []byte) error {
	if err := r.c.checkAccess(ctx, call, tx); err != nil {
		return err
	}
	if err := tx.Put([]byte(r.stKey()), value); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// delete deletes data from the storage engine.
// Performs authorization check.
func (r *rowReq) delete(ctx *context.T, call rpc.ServerCall, tx *watchable.Transaction) error {
	if err := r.c.checkAccess(ctx, call, tx); err != nil {
		return err
	}
	if err := tx.Delete([]byte(r.stKey())); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}
