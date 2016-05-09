// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/longevity_tests/model"
	"v.io/x/ref/services/syncbase/testutil"
)

// CreateDbsAndCollections creates databases and collections according to the
// given models.  It does not fail if any of the databases or collections
// already exist.  If the model contains syncgroups, it will also create or
// join those as well.
func CreateDbsAndCollections(ctx *context.T, sbName string, dbModels model.DatabaseSet) (map[syncbase.Database][]syncbase.Collection, []syncbase.Syncgroup, error) {
	blessing, _ := v23.GetPrincipal(ctx).BlessingStore().Default()
	perms := testutil.DefaultPerms(blessing.String(), "root:checker")
	nsRoots := v23.GetNamespace(ctx).Roots()

	service := syncbase.NewService(sbName)
	syncgroups := []syncbase.Syncgroup{}
	dbColsMap := map[syncbase.Database][]syncbase.Collection{}
	for _, dbModel := range dbModels {
		// Create Database.
		db := service.DatabaseForId(dbModel.Id(), nil)
		// TODO(nlacasse): Don't create the database unless its blessings match
		// ours.
		if err := db.Create(ctx, perms); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
			return nil, nil, err
		}
		dbColsMap[db] = []syncbase.Collection{}

		// Create collections for database.
		for _, colModel := range dbModel.Collections {
			col := db.CollectionForId(colModel.Id())
			// TODO(nlacasse): Don't create the collection unless its blessings
			// match ours.
			if err := col.Create(ctx, perms); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
				return nil, nil, err
			}
			dbColsMap[db] = append(dbColsMap[db], col)
		}

		// Create or join syncgroups for database.
		for _, sgModel := range dbModel.Syncgroups {
			sg := db.SyncgroupForId(wire.Id{Name: sgModel.NameSuffix, Blessing: "blessing"})
			if sgModel.HostDevice.Name == sbName {
				// We are the host.  Create the syncgroup.
				spec := sgModel.Spec()
				spec.MountTables = nsRoots
				// TODO(nlacasse): Set this to something real.
				spec.Perms = testutil.DefaultPerms("root")
				if err := sg.Create(ctx, spec, wire.SyncgroupMemberInfo{}); err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
					return nil, nil, err
				}
				syncgroups = append(syncgroups, sg)
				continue
			}
			// Join the syncgroup.  It might not exist at first, so we loop.
			// TODO(nlacasse): Parameterize number of retries.  Exponential
			// backoff?
			var joinErr error
			for i := 0; i < 10; i++ {
				_, joinErr = sg.Join(ctx, sgModel.HostDevice.Name, "", wire.SyncgroupMemberInfo{})
				if joinErr == nil {
					syncgroups = append(syncgroups, sg)
					break
				} else {
					time.Sleep(100 * time.Millisecond)
				}
			}
			if joinErr != nil {
				return nil, nil, fmt.Errorf("could not join syncgroup %q: %v", sgModel.Name(), joinErr)
			}
		}
	}

	return dbColsMap, syncgroups, nil
}
