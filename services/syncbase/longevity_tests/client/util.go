// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/longevity_tests/model"
	"v.io/x/ref/services/syncbase/testutil"
)

// CreateDbsAndCollections creates databases and collections according to the
// given models.  It does not fail if any of the databases or collections
// already exist.
func CreateDbsAndCollections(ctx *context.T, sbName string, dbModels model.DatabaseSet) (map[syncbase.Database][]syncbase.Collection, error) {
	blessing, _ := v23.GetPrincipal(ctx).BlessingStore().Default()
	perms := testutil.DefaultPerms(blessing.String())

	service := syncbase.NewService(sbName)
	dbColsMap := map[syncbase.Database][]syncbase.Collection{}
	for _, dbModel := range dbModels {
		db := service.DatabaseForId(dbModel.Id(), nil)
		// TODO(nlacasse): Don't create the database unless its blessings match
		// ours.
		if err := db.Create(ctx, perms); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
			return nil, err
		}
		dbColsMap[db] = []syncbase.Collection{}
		for _, colModel := range dbModel.Collections {
			col := db.CollectionForId(colModel.Id())
			// TODO(nlacasse): Don't create the collection unless its blessings
			// match ours.
			if err := col.Create(ctx, perms); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
				return nil, err
			}
			dbColsMap[db] = append(dbColsMap[db], col)
		}
	}

	return dbColsMap, nil
}
