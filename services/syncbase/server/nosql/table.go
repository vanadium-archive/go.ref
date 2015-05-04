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

func (t *table) DeleteRowRange(ctx *context.T, call rpc.ServerCall, start, limit string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (t *table) SetPermissions(ctx *context.T, call rpc.ServerCall, prefix string, perms access.Permissions) error {
	return verror.NewErrNotImplemented(ctx)
}

func (t *table) GetPermissions(ctx *context.T, call rpc.ServerCall, key string) ([]wire.PrefixPermissions, error) {
	return nil, verror.NewErrNotImplemented(ctx)
}

func (t *table) DeletePermissions(ctx *context.T, call rpc.ServerCall, prefix string) error {
	return verror.NewErrNotImplemented(ctx)
}

// TODO(sadovsky): Implement Glob.

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
