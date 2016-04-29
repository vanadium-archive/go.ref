// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"v.io/v23/context"
	"v.io/v23/syncbase"
	"v.io/x/ref/services/syncbase/testutil"
)

// Rows is a map from keys to values.
type Rows map[string]string

// Collections is a map from collection names to Rows.
type Collections map[string]Rows

// Databases is a map from database names to Collections.
type Databases map[string]Collections

// SeedService creates databases, collections, and rows in a syncbase service.
func SeedService(ctx *context.T, s syncbase.Service, dbs Databases) error {
	openPerms := testutil.DefaultPerms("...")
	for dbName, cols := range dbs {
		db := s.Database(ctx, dbName, nil)
		if err := db.Create(ctx, openPerms); err != nil {
			return err
		}

		for colName, rows := range cols {
			col := db.Collection(ctx, colName)
			if err := col.Create(ctx, openPerms); err != nil {
				return err
			}

			for key, val := range rows {
				if err := col.Put(ctx, key, val); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
