// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

// TODO(sadovsky): Handle the case where we're in a batch.

type table struct {
	name string
	d    *database
}

var (
	_ wire.TableServerMethods = (*table)(nil)
	_ util.Layer              = (*table)(nil)
)

////////////////////////////////////////
// RPC methods

func (t *table) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		// Check databaseData perms.
		dData := &databaseData{}
		if err := util.Get(ctx, call, st, t.d, dData); err != nil {
			return err
		}
		// Check for "table already exists".
		if err := util.GetWithoutAuth(ctx, call, st, t, &tableData{}); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
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

func (t *table) Delete(ctx *context.T, call rpc.ServerCall) error {
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		// Read-check-delete tableData.
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			return err
		}
		// TODO(sadovsky): Delete all rows in this table.
		return util.Delete(ctx, call, st, t)
	})
}

func (t *table) DeleteRowRange(ctx *context.T, call rpc.ServerCall, start, end string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (t *table) Scan(ctx *context.T, call wire.TableScanServerCall, start, end string) error {
	sn := t.d.st.NewSnapshot()
	defer sn.Close()
	it := sn.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), start, end))
	sender := call.SendStream()
	key, value := []byte{}, []byte{}
	for it.Advance() {
		key = it.Key(key)
		parts := util.SplitKeyParts(string(key))
		sender.Send(wire.KeyValue{Key: parts[len(parts)-1], Value: it.Value(value)})
	}
	if err := it.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

func (t *table) SetPermissions(ctx *context.T, call rpc.ServerCall, prefix string, perms access.Permissions) error {
	if prefix != "" {
		return verror.NewErrNotImplemented(ctx)
	}
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		data := &tableData{}
		return util.Update(ctx, call, st, t, data, func() error {
			data.Perms = perms
			return nil
		})
	})
}

func (t *table) GetPermissions(ctx *context.T, call rpc.ServerCall, key string) ([]wire.PrefixPermissions, error) {
	if key != "" {
		return nil, verror.NewErrNotImplemented(ctx)
	}
	data := &tableData{}
	if err := util.Get(ctx, call, t.d.st, t, data); err != nil {
		return nil, err
	}
	return []wire.PrefixPermissions{{Prefix: "", Perms: data.Perms}}, nil
}

func (t *table) DeletePermissions(ctx *context.T, call rpc.ServerCall, prefix string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (t *table) Glob__(ctx *context.T, call rpc.ServerCall, pattern string) (<-chan naming.GlobReply, error) {
	// Check perms.
	sn := t.d.st.NewSnapshot()
	if err := util.Get(ctx, call, sn, t, &tableData{}); err != nil {
		sn.Close()
		return nil, err
	}
	return util.Glob(ctx, call, pattern, sn, util.JoinKeyParts(util.RowPrefix, t.name))
}

////////////////////////////////////////
// util.Layer methods

func (t *table) Name() string {
	return t.name
}

func (t *table) StKey() string {
	return util.JoinKeyParts(util.TablePrefix, t.stKeyPart())
}

////////////////////////////////////////
// Internal helpers

func (t *table) stKeyPart() string {
	return t.name
}
