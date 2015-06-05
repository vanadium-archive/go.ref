// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"math/rand"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

// database is a per-database singleton (i.e. not per-request). It does not
// directly handle RPCs.
// Note: If a database does not exist at the time of a database RPC, the
// dispatcher creates a short-lived database object to service that particular
// request.
type database struct {
	name string
	a    interfaces.App
	// The fields below are initialized iff this database exists.
	st store.Store // stores all data for a single database

	// Active snapshots and transactions corresponding to client batches.
	// TODO(sadovsky): Add timeouts and GC.
	mu  sync.Mutex // protects the fields below
	sns map[uint64]store.Snapshot
	txs map[uint64]store.Transaction
}

// databaseReq is a per-request object that handles Database RPCs.
// It embeds database and tracks request-specific batch state.
type databaseReq struct {
	*database
	// If non-nil, sn or tx will be non-nil.
	batchId *uint64
	sn      store.Snapshot
	tx      store.Transaction
}

var (
	_ wire.DatabaseServerMethods = (*databaseReq)(nil)
	_ interfaces.Database        = (*database)(nil)
	_ util.Layer                 = (*database)(nil)
)

// DatabaseOptions configures a database.
type DatabaseOptions struct {
	// Database-level permissions.
	Perms access.Permissions
	// Root dir for data storage.
	RootDir string
	// Storage engine to use.
	Engine string
}

// NewDatabase creates a new database instance and returns it.
// Returns a VDL-compatible error.
// Designed for use from within App.CreateNoSQLDatabase.
func NewDatabase(ctx *context.T, call rpc.ServerCall, a interfaces.App, name string, opts DatabaseOptions) (*database, error) {
	if opts.Perms == nil {
		return nil, verror.New(verror.ErrInternal, ctx, "perms must be specified")
	}
	st, err := util.OpenStore(opts.Engine, path.Join(opts.RootDir, opts.Engine))
	if err != nil {
		return nil, err
	}
	st, err = watchable.Wrap(st, &watchable.Options{
		ManagedPrefixes: []string{util.RowPrefix},
	})
	if err != nil {
		return nil, err
	}
	d := &database{
		name: name,
		a:    a,
		st:   st,
		sns:  make(map[uint64]store.Snapshot),
		txs:  make(map[uint64]store.Transaction),
	}
	data := &databaseData{
		Name:  d.name,
		Perms: opts.Perms,
	}
	if err := util.Put(ctx, call, d.st, d, data); err != nil {
		return nil, err
	}
	return d, nil
}

////////////////////////////////////////
// RPC methods

func (d *databaseReq) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	// This database does not yet exist; d is just an ephemeral handle that holds
	// {name string, a *app}. d.a.CreateNoSQLDatabase will create a new database
	// handle and store it in d.a.dbs[d.name].
	return d.a.CreateNoSQLDatabase(ctx, call, d.name, perms)
}

func (d *databaseReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return d.a.DeleteNoSQLDatabase(ctx, call, d.name)
}

var rng *rand.Rand = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))

func (d *databaseReq) BeginBatch(ctx *context.T, call rpc.ServerCall, bo wire.BatchOptions) (string, error) {
	if d.batchId != nil {
		return "", wire.NewErrBoundToBatch(ctx)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var id uint64
	var batchType string
	for {
		id = uint64(rng.Int63())
		if bo.ReadOnly {
			if _, ok := d.sns[id]; !ok {
				d.sns[id] = d.st.NewSnapshot()
				batchType = "sn"
				break
			}
		} else {
			if _, ok := d.txs[id]; !ok {
				d.txs[id] = d.st.NewTransaction()
				batchType = "tx"
				break
			}
		}
	}
	return strings.Join([]string{d.name, batchType, strconv.FormatUint(id, 10)}, util.BatchSep), nil
}

func (d *databaseReq) Commit(ctx *context.T, call rpc.ServerCall) error {
	if d.batchId == nil {
		return wire.NewErrNotBoundToBatch(ctx)
	}
	if d.tx == nil {
		return wire.NewErrReadOnlyBatch(ctx)
	}
	var err error
	if err = d.tx.Commit(); err == nil {
		d.mu.Lock()
		delete(d.txs, *d.batchId)
		d.mu.Unlock()
	}
	return err
}

func (d *databaseReq) Abort(ctx *context.T, call rpc.ServerCall) error {
	if d.batchId == nil {
		return wire.NewErrNotBoundToBatch(ctx)
	}
	var err error
	if d.tx != nil {
		if err = d.tx.Abort(); err == nil {
			d.mu.Lock()
			delete(d.txs, *d.batchId)
			d.mu.Unlock()
		}
	} else {
		if err = d.sn.Close(); err == nil {
			d.mu.Lock()
			delete(d.sns, *d.batchId)
			d.mu.Unlock()
		}
	}
	return err
}

func (d *databaseReq) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return d.a.SetDatabasePerms(ctx, call, d.name, perms, version)
}

func (d *databaseReq) GetPermissions(ctx *context.T, call rpc.ServerCall) (perms access.Permissions, version string, err error) {
	if d.batchId != nil {
		return nil, "", wire.NewErrBoundToBatch(ctx)
	}
	data := &databaseData{}
	if err := util.Get(ctx, call, d.st, d, data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
}

func (d *databaseReq) GlobChildren__(ctx *context.T, call rpc.ServerCall) (<-chan string, error) {
	if d.batchId != nil {
		return nil, wire.NewErrBoundToBatch(ctx)
	}
	// Check perms.
	sn := d.st.NewSnapshot()
	closeSnapshot := func() error {
		return sn.Close()
	}
	if err := util.Get(ctx, call, sn, d, &databaseData{}); err != nil {
		closeSnapshot()
		return nil, err
	}
	return util.Glob(ctx, call, "*", sn, closeSnapshot, util.TablePrefix)
}

////////////////////////////////////////
// interfaces.Database methods

func (d *database) St() store.Store {
	if d.st == nil {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return d.st
}

func (d *database) App() interfaces.App {
	return d.a
}

func (d *database) CheckPermsInternal(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter) error {
	return util.Get(ctx, call, st, d, &databaseData{})
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

////////////////////////////////////////
// Internal helpers

func (d *databaseReq) batchReader() store.StoreReader {
	if d.batchId == nil {
		return nil
	} else if d.sn != nil {
		return d.sn
	} else {
		return d.tx
	}
}

func (d *databaseReq) batchReadWriter() (store.StoreReadWriter, error) {
	if d.batchId == nil {
		return nil, nil
	} else if d.tx != nil {
		return d.tx, nil
	} else {
		return nil, wire.NewErrReadOnlyBatch(nil)
	}
}
