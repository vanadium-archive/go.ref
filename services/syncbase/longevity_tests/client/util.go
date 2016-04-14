// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"v.io/v23"
	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/testutil"
)

// createDbAndCollections creates a database and set of collections with the
// given names.  It does not fail if the database or any of the collections
// already exist.
func createDbAndCollections(ctx *context.T, sbName, dbName string, collectionIds []wire.Id) (syncbase.Database, []syncbase.Collection, error) {
	blessing, _ := v23.GetPrincipal(ctx).BlessingStore().Default()
	perms := testutil.DefaultPerms(blessing.String())

	service := syncbase.NewService(sbName)
	db := service.Database(ctx, dbName, nil)
	if err := db.Create(ctx, perms); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
		return nil, nil, err
	}

	collections := make([]syncbase.Collection, len(collectionIds))
	for i, id := range collectionIds {
		col := db.CollectionForId(id)
		if err := col.Create(ctx, perms); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
			return nil, nil, err
		}
		collections[i] = col
	}
	return db, collections, nil
}
