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
	"v.io/v23/verror"
)

// rowReq is a per-request object that handles Row RPCs.
type rowReq struct {
	key string
	t   *tableReq
}

var (
	_ wire.RowServerMethods = (*rowReq)(nil)
	_ util.Layer            = (*rowReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (r *rowReq) Get(ctx *context.T, call rpc.ServerCall) ([]byte, error) {
	impl := func(st store.StoreReader) ([]byte, error) {
		return r.get(ctx, call, st)
	}
	var st store.StoreReader
	if r.t.d.batchId != nil {
		st = r.t.d.batchReader()
	} else {
		sn := r.t.d.st.NewSnapshot()
		st = sn
		defer sn.Close()
	}
	return impl(st)
}

func (r *rowReq) Put(ctx *context.T, call rpc.ServerCall, value []byte) error {
	impl := func(st store.StoreReadWriter) error {
		return r.put(ctx, call, st, value)
	}
	if r.t.d.batchId != nil {
		if st, err := r.t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(r.t.d.st, impl)
	}
}

func (r *rowReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	impl := func(st store.StoreReadWriter) error {
		return r.delete(ctx, call, st)
	}
	if r.t.d.batchId != nil {
		if st, err := r.t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(r.t.d.st, impl)
	}
}

////////////////////////////////////////
// util.Layer methods

func (r *rowReq) Name() string {
	return r.key
}

func (r *rowReq) StKey() string {
	return util.JoinKeyParts(util.RowPrefix, r.stKeyPart())
}

////////////////////////////////////////
// Internal helpers

func (r *rowReq) stKeyPart() string {
	return util.JoinKeyParts(r.t.stKeyPart(), r.key)
}

// checkAccess checks that this row's table exists in the database, and performs
// an authorization check.
// Returns a VDL-compatible error.
func (r *rowReq) checkAccess(ctx *context.T, call rpc.ServerCall, st store.StoreReader) error {
	return r.t.checkAccess(ctx, call, st, r.key)
}

// get reads data from the storage engine.
// Performs authorization check.
// Returns a VDL-compatible error.
func (r *rowReq) get(ctx *context.T, call rpc.ServerCall, st store.StoreReader) ([]byte, error) {
	if err := r.checkAccess(ctx, call, st); err != nil {
		return nil, err
	}
	value, err := st.Get([]byte(r.StKey()), nil)
	if err != nil {
		if verror.ErrorID(err) == store.ErrUnknownKey.ID {
			return nil, verror.New(verror.ErrNoExist, ctx, r.Name())
		}
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return value, nil
}

// put writes data to the storage engine.
// Performs authorization check.
// Returns a VDL-compatible error.
func (r *rowReq) put(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter, value []byte) error {
	if err := r.checkAccess(ctx, call, st); err != nil {
		return err
	}
	if err := st.Put([]byte(r.StKey()), value); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// delete deletes data from the storage engine.
// Performs authorization check.
// Returns a VDL-compatible error.
func (r *rowReq) delete(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter) error {
	if err := r.checkAccess(ctx, call, st); err != nil {
		return err
	}
	if err := st.Delete([]byte(r.StKey())); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}
