// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Sync utility functions

import (
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
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
		vlog.Errorf("forEachDatabaseStore: cannot get all app names: %v", err)
		return
	}

	for _, a := range appNames {
		// For each app, get its databases and iterate over them.
		app, err := s.sv.App(ctx, nil, a)
		if err != nil {
			vlog.Errorf("forEachDatabaseStore: cannot get app %s: %v", a, err)
			continue
		}
		dbNames, err := app.NoSQLDatabaseNames(ctx, nil)
		if err != nil {
			vlog.Errorf("forEachDatabaseStore: cannot get all db names for app %s: %v", a, err)
			continue
		}

		for _, d := range dbNames {
			// For each database, get its Store and invoke the callback.
			db, err := app.NoSQLDatabase(ctx, nil, d)
			if err != nil {
				vlog.Errorf("forEachDatabaseStore: cannot get db %s for app %s: %v", d, a, err)
				continue
			}

			if callback(a, d, db.St()) {
				return // done, early exit
			}
		}
	}
}

// getDbStore gets the store handle to the database.
func (s *syncService) getDbStore(ctx *context.T, call rpc.ServerCall, appName, dbName string) (store.Store, error) {
	app, err := s.sv.App(ctx, call, appName)
	if err != nil {
		return nil, err
	}
	db, err := app.NoSQLDatabase(ctx, call, dbName)
	if err != nil {
		return nil, err
	}
	return db.St(), nil
}

// translateError translates store errors.
func translateError(ctx *context.T, err error, key string) error {
	if verror.ErrorID(err) == store.ErrUnknownKey.ID {
		return verror.New(verror.ErrNoExist, ctx, key)
	}
	return verror.New(verror.ErrInternal, ctx, key, err)
}
