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
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

// tableReq is a per-request object that handles Table RPCs.
type tableReq struct {
	name string
	d    *databaseReq
}

var (
	_ wire.TableServerMethods = (*tableReq)(nil)
	_ util.Layer              = (*tableReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (t *tableReq) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		// Check databaseData perms.
		dData := &databaseData{}
		if err := util.Get(ctx, call, st, t.d, dData); err != nil {
			return err
		}
		// Check for "table already exists".
		if err := util.GetWithoutAuth(ctx, call, st, t, &tableData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			// TODO(sadovsky): Should this be ErrExistOrNoAccess, for privacy?
			return verror.New(verror.ErrExist, ctx, t.name)
		}
		// Write new tableData.
		if perms == nil {
			perms = dData.Perms
		}
		data := &tableData{
			Name:  t.name,
			Perms: perms,
		}
		return util.Put(ctx, call, st, t, data)
	})
}

func (t *tableReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		// Read-check-delete tableData.
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}
		// TODO(sadovsky): Delete all rows in this table.
		return util.Delete(ctx, call, st, t)
	})
}

func (t *tableReq) DeleteRowRange(ctx *context.T, call rpc.ServerCall, start, limit []byte) error {
	impl := func(st store.StoreReadWriter) error {
		// Check perms.
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			return err
		}
		it := st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), string(start), string(limit)))
		key := []byte{}
		for it.Advance() {
			key = it.Key(key)
			if err := st.Delete(key); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	if t.d.batchId != nil {
		if st, err := t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) Scan(ctx *context.T, call wire.TableScanServerCall, start, limit []byte) error {
	impl := func(st store.StoreReader) error {
		// Check perms.
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			return err
		}
		it := st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), string(start), string(limit)))
		sender := call.SendStream()
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)
			parts := util.SplitKeyParts(string(key))
			sender.Send(wire.KeyValue{Key: parts[len(parts)-1], Value: value})
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	var st store.StoreReader
	if t.d.batchId != nil {
		st = t.d.batchReader()
	} else {
		sn := t.d.st.NewSnapshot()
		st = sn
		defer sn.Close()
	}
	return impl(st)
}

func (t *tableReq) SetPermissions(ctx *context.T, call rpc.ServerCall, prefix string, perms access.Permissions) error {
	if prefix != "" {
		return verror.NewErrNotImplemented(ctx)
	}
	impl := func(st store.StoreReadWriter) error {
		data := &tableData{}
		return util.Update(ctx, call, st, t, data, func() error {
			data.Perms = perms
			return nil
		})
	}
	if t.d.batchId != nil {
		if st, err := t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) GetPermissions(ctx *context.T, call rpc.ServerCall, key string) ([]wire.PrefixPermissions, error) {
	if key != "" {
		return nil, verror.NewErrNotImplemented(ctx)
	}
	impl := func(st store.StoreReader) ([]wire.PrefixPermissions, error) {
		data := &tableData{}
		if err := util.Get(ctx, call, t.d.st, t, data); err != nil {
			return nil, err
		}
		return []wire.PrefixPermissions{{Prefix: "", Perms: data.Perms}}, nil
	}
	var st store.StoreReader
	if t.d.batchId != nil {
		st = t.d.batchReader()
	} else {
		sn := t.d.st.NewSnapshot()
		st = sn
		defer sn.Close()
	}
	return impl(st)
}

func (t *tableReq) DeletePermissions(ctx *context.T, call rpc.ServerCall, prefix string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (t *tableReq) GlobChildren__(ctx *context.T, call rpc.ServerCall) (<-chan string, error) {
	impl := func(st store.StoreReader, closeStoreReader func() error) (<-chan string, error) {
		// Check perms.
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			closeStoreReader()
			return nil, err
		}
		return util.Glob(ctx, call, "*", st, closeStoreReader, util.JoinKeyParts(util.RowPrefix, t.name))
	}
	var st store.StoreReader
	var closeStoreReader func() error
	if t.d.batchId != nil {
		st = t.d.batchReader()
		closeStoreReader = func() error {
			return nil
		}
	} else {
		sn := t.d.st.NewSnapshot()
		st = sn
		closeStoreReader = func() error {
			return sn.Close()
		}
	}
	return impl(st, closeStoreReader)
}

////////////////////////////////////////
// util.Layer methods

func (t *tableReq) Name() string {
	return t.name
}

func (t *tableReq) StKey() string {
	return util.JoinKeyParts(util.TablePrefix, t.stKeyPart())
}

////////////////////////////////////////
// Internal helpers

func (t *tableReq) stKeyPart() string {
	return t.name
}
