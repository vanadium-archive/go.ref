// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sbtree_test

import (
	"testing"

	"v.io/v23/syncbase"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/debug/debug/browseserver/sbtree"
	tu "v.io/x/ref/services/syncbase/testutil"
	"v.io/x/ref/test"
)

func TestWithNoServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test, because has long timeout.")
	}
	ctx, cleanup := test.V23Init()
	defer cleanup()

	_, err := sbtree.AssembleSyncbaseTree(ctx, "no-such-server")

	if err == nil || err == sbtree.NoSyncbaseError {
		t.Errorf("Got %v, want not nil and not %v", err, sbtree.NoSyncbaseError)
	}
}

func TestWithEmptyService(t *testing.T) {
	ctx, serverName, cleanup := tu.SetupOrDie(nil)
	defer cleanup()

	got, err := sbtree.AssembleSyncbaseTree(ctx, serverName)
	if err != nil {
		t.Fatal(err)
	}

	if got.Service.FullName() != serverName {
		t.Errorf("got %q, want %q", got.Service.FullName(), serverName)
	}
	if len(got.Dbs) != 0 {
		t.Errorf("want no databases, got %v", got.Dbs)
	}
}

func TestWithMultipleEmptyDbs(t *testing.T) {
	ctx, serverName, cleanup := tu.SetupOrDie(nil)
	defer cleanup()
	var (
		service = syncbase.NewService(serverName)
		dbNames = []string{"db_a", "db_b", "db_c"}
	)
	for _, dbName := range dbNames {
		tu.CreateDatabase(t, ctx, service, dbName)
	}

	got, err := sbtree.AssembleSyncbaseTree(ctx, serverName)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Dbs) != 3 {
		t.Fatalf("want 3 databases, got %v", got.Dbs)
	}
	for i, db := range got.Dbs {
		if db.Database.Id().Name != dbNames[i] {
			t.Errorf("got %q, want %q", db.Database.Id().Name, dbNames[i])
		}
		if len(db.Collections) != 0 {
			t.Errorf("want no collections, got %v", db.Collections)
		}
		if len(db.Syncgroups) != 0 {
			t.Errorf("want no syncgroups, got %v", db.Syncgroups)
		}
	}
}

func TestWithMultipleCollections(t *testing.T) {
	ctx, serverName, cleanup := tu.SetupOrDie(nil)
	defer cleanup()
	var (
		service   = syncbase.NewService(serverName)
		collNames = []string{"coll_a", "coll_b", "coll_c"}
		database  = tu.CreateDatabase(t, ctx, service, "the_db")
	)
	for _, collName := range collNames {
		tu.CreateCollection(t, ctx, database, collName)
	}

	got, err := sbtree.AssembleSyncbaseTree(ctx, serverName)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Dbs) != 1 {
		t.Fatalf("want 1 database, got %v", got.Dbs)
	}
	for i, coll := range got.Dbs[0].Collections {
		if coll.Id().Name != collNames[i] {
			t.Errorf("got %q, want %q", coll.Id().Name, collNames[i])
		}
		exists, err := coll.Exists(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Error("want collection to exist, but it does not")
		}
	}
}

func TestWithMultipleSyncgroups(t *testing.T) {
	ctx, serverName, cleanup := tu.SetupOrDie(nil)
	defer cleanup()
	var (
		service        = syncbase.NewService(serverName)
		sgNames        = []string{"syncgroup_a", "syncgroup_b", "syncgroup_c"}
		sgDescriptions = []string{"AAA", "BBB", "CCC"}
		database       = tu.CreateDatabase(t, ctx, service, "the_db")
		coll           = tu.CreateCollection(t, ctx, database, "the_collection")
	)
	for i, sgName := range sgNames {
		tu.CreateSyncgroup(t, ctx, database, coll, sgName, sgDescriptions[i])
	}

	got, err := sbtree.AssembleSyncbaseTree(ctx, serverName)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Dbs) != 1 {
		t.Fatalf("want 1 database, got %v", got.Dbs)
	}
	for i, sg := range got.Dbs[0].Syncgroups {
		if sg.Syncgroup.Id().Name != sgNames[i] {
			t.Errorf("got %q, want %q", sg.Syncgroup.Id().Name, sgNames[i])
		}
		if sg.Spec.Description != sgDescriptions[i] {
			t.Errorf("got %q, want %q", sg.Spec.Description, sgDescriptions[i])
		}
		if sg.Spec.Collections[0].Name != "the_collection" {
			t.Errorf(`got %q, want "the_collection"`,
				sg.Spec.Collections[0].Name)
		}
	}
}
