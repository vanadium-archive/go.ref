// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util_test

import (
	"reflect"
	"testing"

	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	sbUtil "v.io/v23/syncbase/util"
	"v.io/v23/vdl"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/longevity_tests/util"
	"v.io/x/ref/services/syncbase/testutil"
)

func TestDumpStream(t *testing.T) {
	ctx, serverName, cleanup := testutil.SetupOrDie(nil)
	defer cleanup()

	dbs := util.Databases{
		"db1": util.Collections{
			"col1": util.Rows{
				"key1": "val1",
				"key2": "val2",
				"key3": "val3",
			},
			"col2": util.Rows{},
		},
		"db2": util.Collections{},
		"db3": util.Collections{
			"col3": util.Rows{},
			"col4": util.Rows{
				"key4": "val4",
				"key5": "val5",
				"key6": "val6",
			},
		},
	}

	// Seed service with databases defined above.
	service := syncbase.NewService(serverName)
	if err := util.SeedService(ctx, service, dbs); err != nil {
		t.Fatal(err)
	}

	// Get expected blessings for database and collection.
	dbBlessing, err := sbUtil.AppBlessingFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	colBlessing, err := sbUtil.UserBlessingFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Get expected Ids for database and collection.
	db1Id := wire.Id{Name: "db1", Blessing: dbBlessing}
	db2Id := wire.Id{Name: "db2", Blessing: dbBlessing}
	db3Id := wire.Id{Name: "db3", Blessing: dbBlessing}
	col1Id := wire.Id{Name: "col1", Blessing: colBlessing}
	col2Id := wire.Id{Name: "col2", Blessing: colBlessing}
	col3Id := wire.Id{Name: "col3", Blessing: colBlessing}
	col4Id := wire.Id{Name: "col4", Blessing: colBlessing}

	// Expected rows from dump stream.
	wantRows := []util.Row{
		util.Row{DatabaseId: db1Id},
		util.Row{DatabaseId: db1Id, CollectionId: col1Id},
		util.Row{DatabaseId: db1Id, CollectionId: col1Id, Key: "key1", Value: vdl.StringValue(nil, "val1")},
		util.Row{DatabaseId: db1Id, CollectionId: col1Id, Key: "key2", Value: vdl.StringValue(nil, "val2")},
		util.Row{DatabaseId: db1Id, CollectionId: col1Id, Key: "key3", Value: vdl.StringValue(nil, "val3")},
		util.Row{DatabaseId: db1Id, CollectionId: col2Id},

		util.Row{DatabaseId: db2Id},

		util.Row{DatabaseId: db3Id},
		util.Row{DatabaseId: db3Id, CollectionId: col3Id},
		util.Row{DatabaseId: db3Id, CollectionId: col4Id},
		util.Row{DatabaseId: db3Id, CollectionId: col4Id, Key: "key4", Value: vdl.StringValue(nil, "val4")},
		util.Row{DatabaseId: db3Id, CollectionId: col4Id, Key: "key5", Value: vdl.StringValue(nil, "val5")},
		util.Row{DatabaseId: db3Id, CollectionId: col4Id, Key: "key6", Value: vdl.StringValue(nil, "val6")},
	}

	dumpStreamMatches(t, ctx, service, wantRows)
}

func dumpStreamMatches(t *testing.T, ctx *context.T, service syncbase.Service, wantRows []util.Row) {
	stream, err := util.NewDumpStream(ctx, service)
	if err != nil {
		t.Fatalf("NewDumpStream failed: %v", err)
	}

	gotRows := []util.Row{}
	for stream.Advance() {
		if stream.Err() != nil {
			t.Fatalf("stream.Err() returned error: %v", err)
		}

		gotRows = append(gotRows, *stream.Row())
	}

	if !reflect.DeepEqual(wantRows, gotRows) {
		t.Errorf("wanted rows %v but got %v", wantRows, gotRows)
	}
}
