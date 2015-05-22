// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/memstore"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

type database struct {
	name string
	a    interfaces.App
	// The fields below are initialized iff this database exists.
	st store.Store // stores all data for a single database
}

var (
	_ wire.DatabaseServerMethods = (*database)(nil)
	_ interfaces.Database        = (*database)(nil)
	_ util.Layer                 = (*database)(nil)
)

// NewDatabase creates a new database instance and returns it.
// Returns a VDL-compatible error.
// Designed for use from within App.CreateNoSQLDatabase.
func NewDatabase(ctx *context.T, call rpc.ServerCall, a interfaces.App, name string, perms access.Permissions) (*database, error) {
	if perms == nil {
		return nil, verror.New(verror.ErrInternal, ctx, "perms must be specified")
	}
	// TODO(sadovsky): Make storage engine pluggable.
	d := &database{
		name: name,
		a:    a,
		st:   memstore.New(),
	}
	data := &databaseData{
		Name:  d.name,
		Perms: perms,
	}
	if err := util.Put(ctx, call, d.st, d, data); err != nil {
		return nil, err
	}
	return d, nil
}

////////////////////////////////////////
// RPC methods

func (d *database) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	// This database does not yet exist; d is just an ephemeral handle that holds
	// {name string, a *app}. d.a.CreateNoSQLDatabase will create a new database
	// handle and store it in d.a.dbs[d.name].
	return d.a.CreateNoSQLDatabase(ctx, call, d.name, perms)
}

func (d *database) Delete(ctx *context.T, call rpc.ServerCall) error {
	return d.a.DeleteNoSQLDatabase(ctx, call, d.name)
}

func (d *database) BeginBatch(ctx *context.T, call rpc.ServerCall, bo wire.BatchOptions) (string, error) {
	return "", verror.NewErrNotImplemented(ctx)
}

func (d *database) Commit(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) Abort(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	return d.a.SetDatabasePerms(ctx, call, d.name, perms, version)
}

func (d *database) GetPermissions(ctx *context.T, call rpc.ServerCall) (perms access.Permissions, version string, err error) {
	data := &databaseData{}
	if err := util.Get(ctx, call, d.st, d, data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
}

func (d *database) Glob__(ctx *context.T, call rpc.ServerCall, pattern string) (<-chan naming.GlobReply, error) {
	// Check perms.
	sn := d.st.NewSnapshot()
	if err := util.Get(ctx, call, sn, d, &databaseData{}); err != nil {
		sn.Close()
		return nil, err
	}
	return util.Glob(ctx, call, pattern, sn, util.TablePrefix)
}

////////////////////////////////////////
// interfaces.Database methods

func (d *database) St() store.Store {
	if d.st == nil {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return d.st
}

func (d *database) CheckPermsInternal(ctx *context.T, call rpc.ServerCall) error {
	if d.st == nil {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return util.Get(ctx, call, d.st, d, &databaseData{})
}

func (d *database) SetPermsInternal(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	if d.st == nil {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return store.RunInTransaction(d.st, func(st store.StoreReadWriter) error {
		data := &databaseData{}
		return util.Update(ctx, call, st, d, data, func() error {
			if err := util.CheckVersion(ctx, version, data.Version); err != nil {
				return err
			}
			data.Perms = perms
			data.Version++
			return nil
		})
	})
}

////////////////////////////////////////
// util.Layer methods

func (d *database) Name() string {
	return d.name
}

func (d *database) StKey() string {
	return util.DatabasePrefix
}
