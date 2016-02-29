// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	wire "v.io/v23/services/syncbase"
	nosqlWire "v.io/v23/services/syncbase/nosql"
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

type dispatcher struct {
	a interfaces.App
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(a interfaces.App) *dispatcher {
	return &dispatcher{a: a}
}

// We always return an AllowEveryone authorizer from Lookup(), and rely on our
// RPC method implementations to perform proper authorization.
var auth security.Authorizer = security.AllowEveryone()

// Note that our client libraries escape component names (app/db/table names,
// row keys) embedded in object names, and our server dispatchers immediately
// unescape them. The only parts of the Syncbase implementation that must be
// aware of this escaping and unescaping are those parts that deal in object
// names.
// This approach confers the following benefits:
// - the scan and exec implementations need not be aware of key escaping;
// - row locality corresponds to the lexicographic order of unescaped keys (as
//   clients would expect); and
// - the object names returned by glob, which are escaped by our GlobChildren
//   implementation, are always valid component names.
//
// TODO(sadovsky): Escape app, db, and table names (using an aggressive
// escaping) when persisting them in underlying storage engines, to prevent
// shadowing. This is not yet a problem because database and table names are
// restricted to be valid identifiers, such that neither "<appName>:<dbName>"
// nor "<tableName>:<rowKey>" can be ambiguous.
func (disp *dispatcher) Lookup(ctx *context.T, suffix string) (interface{}, security.Authorizer, error) {
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
	if dInt, err := disp.a.NoSQLDatabase(nil, nil, dbName); err == nil {
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
				a:    disp.a,
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
		return nosqlWire.DatabaseServer(dReq), auth, nil
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
		return nosqlWire.TableServer(tReq), auth, nil
	}

	rReq := &rowReq{
		key: rowKey,
		t:   tReq,
	}
	if len(parts) == 3 {
		return nosqlWire.RowServer(rReq), auth, nil
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
