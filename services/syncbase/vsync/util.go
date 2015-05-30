// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Sync utility functions

import (
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
)

// forEachDatabaseStore iterates over all Databases in all Apps within the
// service and invokes the provided callback function on each database. The
// callback returns a "done" flag to make forEachDatabaseStore() stop the
// iteration earlier; otherwise the function loops across all databases of all
// apps.
func (s *syncService) forEachDatabaseStore(ctx *context.T, callback func(store.Store) bool) {
	// Get the apps and iterate over them.
	// TODO(rdaoud): use a "privileged call" parameter instead of nil (here and
	// elsewhere).
	appNames, err := s.sv.AppNames(ctx, nil)
	if err != nil {
		return
	}

	for _, a := range appNames {
		// For each app, get its databases and iterate over them.
		app, err := s.sv.App(ctx, nil, a)
		if err != nil {
			continue
		}
		dbNames, err := app.NoSQLDatabaseNames(ctx, nil)
		if err != nil {
			continue
		}

		for _, d := range dbNames {
			// For each database, get its Store and invoke the callback.
			db, err := app.NoSQLDatabase(ctx, nil, d)
			if err != nil {
				continue
			}

			if callback(db.St()) {
				return // done, early exit
			}
		}
	}
}
