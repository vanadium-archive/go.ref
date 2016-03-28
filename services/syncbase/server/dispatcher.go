// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	wire "v.io/v23/services/syncbase"
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

type dispatcher struct {
	s *service
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(s *service) *dispatcher {
	return &dispatcher{s: s}
}

// We always return an AllowEveryone authorizer from Lookup(), and rely on our
// RPC method implementations to perform proper authorization.
var auth security.Authorizer = security.AllowEveryone()

func (disp *dispatcher) Lookup(ctx *context.T, suffix string) (interface{}, security.Authorizer, error) {
	if len(suffix) == 0 {
		return wire.ServiceServer(disp.s), auth, nil
	}
	parts := strings.SplitN(suffix, "/", 2)

	// If the first slash-separated component of suffix is SyncbaseSuffix,
	// dispatch to the sync module.
	if parts[0] == common.SyncbaseSuffix {
		return interfaces.SyncServer(disp.s.sync), auth, nil
	}

	// Validate all name components up front, so that we can avoid doing so in all
	// our method implementations.
	escAppName := parts[0]
	appName, ok := pubutil.Unescape(escAppName)
	if !ok || !pubutil.ValidAppName(appName) {
		return nil, nil, wire.NewErrInvalidName(ctx, suffix)
	}

	appExists := false
	var a *app
	if aInt, err := disp.s.App(nil, nil, appName); err == nil {
		a = aInt.(*app) // panics on failure, as desired
		appExists = true
	} else {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return nil, nil, err
		} else {
			a = &app{
				name: appName,
				s:    disp.s,
			}
		}
	}

	if len(parts) == 1 {
		return wire.AppServer(a), auth, nil
	}

	// All database, table, and row methods require the app to exist. If it
	// doesn't, abort early.
	if !appExists {
		return nil, nil, verror.New(verror.ErrNoExist, ctx, a.name)
	}

	// Note, it's possible for the app to be deleted concurrently with downstream
	// handling of this request. Depending on the order in which things execute,
	// the client may not get an error, but in any case ultimately the store will
	// end up in a consistent state.
	return disp.LookupBeyondApp(ctx, a, parts[1])
}

// TODO(sadovsky): Migrated from "nosql" package. Merge with code above.
func (disp *dispatcher) LookupBeyondApp(ctx *context.T, a interfaces.App, suffix string) (interface{}, security.Authorizer, error) {
	parts := strings.SplitN(suffix, "/", 3) // db, table, row

	// Note, the slice returned by strings.SplitN is guaranteed to contain at
	// least one element.
	dbParts := strings.SplitN(parts[0], common.BatchSep, 2)
	escDbName := dbParts[0]

	// Validate all name components up front, so that we can avoid doing so in all
	// our method implementations.
	dbName, ok := pubutil.Unescape(escDbName)
	if !ok || !pubutil.ValidDatabaseName(dbName) {
		return nil, nil, wire.NewErrInvalidName(ctx, suffix)
	}

	var tableName, rowKey string
	if len(parts) > 1 {
		tableName, ok = pubutil.Unescape(parts[1])
		if !ok || !pubutil.ValidTableName(tableName) {
			return nil, nil, wire.NewErrInvalidName(ctx, suffix)
		}
	}

	if len(parts) > 2 {
		rowKey, ok = pubutil.Unescape(parts[2])
		if !ok || !pubutil.ValidRowKey(rowKey) {
			return nil, nil, wire.NewErrInvalidName(ctx, suffix)
		}
	}

	dbExists := false
	var d *database
	if dInt, err := a.Database(nil, nil, dbName); err == nil {
		d = dInt.(*database) // panics on failure, as desired
		dbExists = true
	} else {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return nil, nil, err
		} else {
			// Database does not exist. Create a short-lived database object to
			// service this request.
			d = &database{
				name: dbName,
				a:    a,
			}
		}
	}

	dReq := &databaseReq{database: d}
	if len(dbParts) == 2 {
		if err := setBatchFields(ctx, dReq, dbParts[1]); err != nil {
			return nil, nil, err
		}
	}
	if len(parts) == 1 {
		return wire.DatabaseServer(dReq), auth, nil
	}

	// All table and row methods require the database to exist. If it doesn't,
	// abort early.
	if !dbExists {
		return nil, nil, verror.New(verror.ErrNoExist, ctx, d.name)
	}

	// Note, it's possible for the database to be deleted concurrently with
	// downstream handling of this request. Depending on the order in which things
	// execute, the client may not get an error, but in any case ultimately the
	// store will end up in a consistent state.
	tReq := &tableReq{
		name: tableName,
		d:    dReq,
	}
	if len(parts) == 2 {
		return wire.TableServer(tReq), auth, nil
	}

	rReq := &rowReq{
		key: rowKey,
		t:   tReq,
	}
	if len(parts) == 3 {
		return wire.RowServer(rReq), auth, nil
	}

	return nil, nil, verror.NewErrNoExist(ctx)
}

// setBatchFields sets the batch-related fields in databaseReq based on the
// value of batchInfo (suffix of the database name component). It returns an error
// if batchInfo is malformed or the batch does not exist.
func setBatchFields(ctx *context.T, d *databaseReq, batchInfo string) error {
	// TODO(sadovsky): Maybe share a common keyspace between sns and txs so that
	// we can avoid including the batch type in the batchInfo string.
	batchType, batchId, err := common.SplitBatchInfo(batchInfo)
	if err != nil {
		return err
	}
	d.batchId = &batchId
	d.mu.Lock()
	defer d.mu.Unlock()
	var ok bool
	switch batchType {
	case common.BatchTypeSn:
		d.sn, ok = d.sns[batchId]
	case common.BatchTypeTx:
		d.tx, ok = d.txs[batchId]
	}
	if !ok {
		return wire.NewErrUnknownBatch(ctx)
	}
	return nil
}
