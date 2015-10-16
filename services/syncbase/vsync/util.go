// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Sync utility functions

import (
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase/nosql"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

const (
	nanoPerSec = int64(1000000000)
)

// forEachDatabaseStore iterates over all Databases in all Apps within the
// service and invokes the callback function on each database. The callback
// returns a "done" flag to make forEachDatabaseStore() stop the iteration
// earlier; otherwise the function loops across all databases of all apps.
func (s *syncService) forEachDatabaseStore(ctx *context.T, callback func(string, string, store.Store) bool) {
	// Get the apps and iterate over them.
	// TODO(rdaoud): use a "privileged call" parameter instead of nil (here and
	// elsewhere).
	appNames, err := s.sv.AppNames(ctx, nil)
	if err != nil {
		vlog.Errorf("sync: forEachDatabaseStore: cannot get all app names: %v", err)
		return
	}

	for _, a := range appNames {
		// For each app, get its databases and iterate over them.
		app, err := s.sv.App(ctx, nil, a)
		if err != nil {
			vlog.Errorf("sync: forEachDatabaseStore: cannot get app %s: %v", a, err)
			continue
		}
		dbNames, err := app.NoSQLDatabaseNames(ctx, nil)
		if err != nil {
			vlog.Errorf("sync: forEachDatabaseStore: cannot get all db names for app %s: %v", a, err)
			continue
		}

		for _, d := range dbNames {
			// For each database, get its Store and invoke the callback.
			db, err := app.NoSQLDatabase(ctx, nil, d)
			if err != nil {
				vlog.Errorf("sync: forEachDatabaseStore: cannot get db %s for app %s: %v", d, a, err)
				continue
			}

			if callback(a, d, db.St()) {
				return // done, early exit
			}
		}
	}
}

// getDb gets the database handle.
func (s *syncService) getDb(ctx *context.T, call rpc.ServerCall, appName, dbName string) (interfaces.Database, error) {
	app, err := s.sv.App(ctx, call, appName)
	if err != nil {
		return nil, err
	}
	return app.NoSQLDatabase(ctx, call, dbName)
}

// getDbStore gets the store handle to the database.
func (s *syncService) getDbStore(ctx *context.T, call rpc.ServerCall, appName, dbName string) (store.Store, error) {
	db, err := s.getDb(ctx, call, appName, dbName)
	if err != nil {
		return nil, err
	}
	return db.St(), nil
}

// unixNanoToTime converts a Unix timestamp in nanoseconds to a Time object.
func unixNanoToTime(timestamp int64) time.Time {
	if timestamp < 0 {
		vlog.Fatalf("sync: unixNanoToTime: invalid timestamp %d", timestamp)
	}
	return time.Unix(timestamp/nanoPerSec, timestamp%nanoPerSec)
}

// toTableRowPrefixStr converts a SyncgroupPrefix (tableName-rowPrefix pair) to
// a string of the form used for storing perms and row data in the underlying
// storage engine.
func toTableRowPrefixStr(p wire.SyncgroupPrefix) string {
	return util.JoinKeyParts(p.TableName, p.RowPrefix)
}

// TODO(jlodhia): extractAppKey() method is temporary for conflict resolution.
// Will be removed once SyncgroupPrefix is refactored into a generic
// TableRow struct.
// extractAppKey extracts the app key from the key sent over the wire between
// two Syncbases. The on-wire key starts with one of the store's reserved
// prefixes for managed namespaces (e.g. $row, $perms). This function removes
// that prefix and returns the application component of the key. This is done
// typically before comparing keys with the SyncGroup prefixes which are defined
// by the application.
func extractAppKey(key string) string {
	parts := splitKeyIntoParts(key, 2)
	return util.JoinKeyParts(parts[1:]...)
}

// isRowKey checks if the given key belongs to a data row.
func isRowKey(key string) bool {
	return strings.HasPrefix(key, util.RowPrefix)
}

// makeRowKey takes an app key, whose structure is <table>:<row>, and converts
// it into store's representation of row key with structure $row:<table>:<row>
func toRowKey(appKey string) string {
	return util.JoinKeyParts(util.RowPrefix, appKey)
}

// Returns the table name and key within the table from the given row key.
func extractComponentsFromKey(key string) (table string, row string) {
	parts := splitKeyIntoParts(key, 3)
	return parts[1], parts[2]
}

func splitKeyIntoParts(key string, minCount int) []string {
	parts := util.SplitKeyParts(key)
	if len(parts) < minCount {
		vlog.Fatalf("sync: extractKeyParts: invalid entry key %s (expected %d parts)", key, minCount)
	}
	return parts
}
