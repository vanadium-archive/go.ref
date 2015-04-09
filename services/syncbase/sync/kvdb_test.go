// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Tests for the Veyron Sync K/V DB component.

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

// A user structure stores info in the "users" table.
type user struct {
	Username string
	Drinks   []string
}

// A drink structure stores info in the "drinks" table.
type drink struct {
	Name    string
	Alcohol bool
}

var (
	users = []user{
		{Username: "lancelot", Drinks: []string{"beer", "coffee"}},
		{Username: "arthur", Drinks: []string{"coke", "beer", "coffee"}},
		{Username: "robin", Drinks: []string{"pepsi"}},
		{Username: "galahad"},
	}
	drinks = []drink{
		{Name: "coke", Alcohol: false},
		{Name: "pepsi", Alcohol: false},
		{Name: "beer", Alcohol: true},
		{Name: "coffee", Alcohol: false},
	}
)

// createTestDB creates a K/V DB with 2 tables.
func createTestDB(t *testing.T) (fname string, db *kvdb, usersTbl, drinksTbl *kvtable) {
	fname = fmt.Sprintf("%s/sync_kvdb_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())
	db, tbls, err := kvdbOpen(fname, []string{"users", "drinks"})
	if err != nil {
		os.Remove(fname)
		t.Fatalf("cannot create new K/V DB file %s: %v", fname, err)
	}

	usersTbl, drinksTbl = tbls[0], tbls[1]
	return
}

// initTestTables initializes the K/V tables used by the tests.
func initTestTables(t *testing.T, usersTbl, drinksTbl *kvtable, useCreate bool) {
	userPut, drinkPut, funcName := usersTbl.set, drinksTbl.set, "set()"
	if useCreate {
		userPut, drinkPut, funcName = usersTbl.create, drinksTbl.create, "create()"
	}

	for _, uu := range users {
		if err := userPut(uu.Username, &uu); err != nil {
			t.Fatalf("%s failed for user %s", funcName, uu.Username)
		}
	}

	for _, dd := range drinks {
		if err := drinkPut(dd.Name, &dd); err != nil {
			t.Fatalf("%s failed for drink %s", funcName, dd.Name)
		}
	}

	return
}

// checkTestTables verifies the contents of the K/V tables.
func checkTestTables(t *testing.T, usersTbl, drinksTbl *kvtable) {
	for _, uu := range users {
		var u2 user
		if err := usersTbl.get(uu.Username, &u2); err != nil {
			t.Fatalf("get() failed for user %s", uu.Username)
		}
		if !reflect.DeepEqual(u2, uu) {
			t.Fatalf("got wrong data for user %s: %#v instead of %#v", uu.Username, u2, uu)
		}
		if !usersTbl.hasKey(uu.Username) {
			t.Fatalf("hasKey() did not find user %s", uu.Username)
		}
	}
	for _, dd := range drinks {
		var d2 drink
		if err := drinksTbl.get(dd.Name, &d2); err != nil {
			t.Fatalf("get() failed for drink %s", dd.Name)
		}
		if !reflect.DeepEqual(d2, dd) {
			t.Fatalf("got wrong data for drink %s: %#v instead of %#v", dd.Name, d2, dd)
		}
		if !drinksTbl.hasKey(dd.Name) {
			t.Fatalf("hasKey() did not find drink %s", dd.Name)
		}
	}

	if num := usersTbl.getNumKeys(); num != uint64(len(users)) {
		t.Fatalf("getNumKeys(): wrong user count: got %v instead of %v", num, len(users))
	}
	if num := drinksTbl.getNumKeys(); num != uint64(len(drinks)) {
		t.Fatalf("getNumKeys(): wrong drink count: got %v instead of %v", num, len(drinks))
	}
}

func TestKVDBSet(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, false)

	db.flush()
}

func TestKVDBCreate(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, true)

	db.flush()
}

func TestKVDBBadGet(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	// The DB is empty, all gets must fail.
	for _, uu := range users {
		var u2 user
		if err := usersTbl.get(uu.Username, &u2); err == nil {
			t.Fatalf("get() found non-existent user %s in file %s: %v", uu.Username, kvdbfile, u2)
		}
	}
	for _, dd := range drinks {
		var d2 drink
		if err := drinksTbl.get(dd.Name, &d2); err == nil {
			t.Fatalf("get() found non-existent drink %s in file %s: %v", dd.Name, kvdbfile, d2)
		}
	}
}

func TestKVDBBadUpdate(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	// The DB is empty, all updates must fail.
	for _, uu := range users {
		u2 := user{Username: uu.Username}
		if err := usersTbl.update(uu.Username, &u2); err == nil {
			t.Fatalf("update() worked for a non-existent user %s in file %s", uu.Username, kvdbfile)
		}
	}
	for _, dd := range drinks {
		d2 := drink{Name: dd.Name}
		if err := drinksTbl.update(dd.Name, &d2); err == nil {
			t.Fatalf("update() worked for a non-existent drink %s in file %s", dd.Name, kvdbfile)
		}
	}
}

func TestKVDBBadHasKey(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	// The DB is empty, all key-checks must fail.
	for _, uu := range users {
		if usersTbl.hasKey(uu.Username) {
			t.Fatalf("hasKey() found non-existent user %s in file %s", uu.Username, kvdbfile)
		}
	}
	for _, dd := range drinks {
		if drinksTbl.hasKey(dd.Name) {
			t.Fatalf("hasKey() found non-existent drink %s in file %s", dd.Name, kvdbfile)
		}
	}
}

func TestKVDBGet(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, false)
	checkTestTables(t, usersTbl, drinksTbl)

	db.flush()
	checkTestTables(t, usersTbl, drinksTbl)
}

func TestKVDBBadCreate(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, false)

	// Must not be able to re-create the same entries.
	for _, uu := range users {
		u2 := user{Username: uu.Username}
		if err := usersTbl.create(uu.Username, &u2); err == nil {
			t.Fatalf("create() worked for an existing user %s in file %s", uu.Username, kvdbfile)
		}
	}
	for _, dd := range drinks {
		d2 := drink{Name: dd.Name}
		if err := drinksTbl.create(dd.Name, &d2); err == nil {
			t.Fatalf("create() worked for an existing drink %s in file %s", dd.Name, kvdbfile)
		}
	}

	db.flush()
}

func TestKVDBReopen(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)

	initTestTables(t, usersTbl, drinksTbl, true)

	// Close the re-open the file.
	db.flush()
	db.close()

	db, tbls, err := kvdbOpen(kvdbfile, []string{"users", "drinks"})
	if err != nil {
		t.Fatalf("Cannot re-open existing K/V DB file %s", kvdbfile)
	}
	defer db.close()

	usersTbl, drinksTbl = tbls[0], tbls[1]
	checkTestTables(t, usersTbl, drinksTbl)
}

func TestKVDBKeyIter(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, false)

	// Get the list of all entry keys in each table.
	keylist := ""
	err := usersTbl.keyIter(func(key string) {
		keylist += key + ","
	})
	if err != nil || keylist != "arthur,galahad,lancelot,robin," {
		t.Fatalf("keyIter() failed in file %s: err %v, user names: %v", kvdbfile, err, keylist)
	}
	keylist = ""
	err = drinksTbl.keyIter(func(key string) {
		keylist += key + ","
	})
	if err != nil || keylist != "beer,coffee,coke,pepsi," {
		t.Fatalf("keyIter() failed in file %s: err %v, drink names: %v", kvdbfile, err, keylist)
	}

	db.flush()
}

func TestKVDBUpdate(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)

	initTestTables(t, usersTbl, drinksTbl, false)
	db.flush()
	db.close()

	db, tbls, err := kvdbOpen(kvdbfile, []string{"users", "drinks"})
	if err != nil {
		t.Fatalf("Cannot re-open existing K/V DB file %s", kvdbfile)
	}
	defer db.close()

	usersTbl, drinksTbl = tbls[0], tbls[1]

	for _, uu := range users {
		key := uu.Username
		u2 := uu
		u2.Username += "-New"

		if err = usersTbl.update(key, &u2); err != nil {
			t.Fatalf("update() failed for user %s in file %s", key, kvdbfile)
		}

		var u3 user
		if err = usersTbl.get(key, &u3); err != nil {
			t.Fatalf("get() failed for user %s in file %s", key, kvdbfile)
		}
		if !reflect.DeepEqual(u3, u2) {
			t.Fatalf("got wrong new data for user %s in file %s: %#v instead of %#v", key, kvdbfile, u3, u2)
		}
	}

	for _, dd := range drinks {
		key := dd.Name
		d2 := dd
		d2.Alcohol = !d2.Alcohol

		if err = drinksTbl.update(key, &d2); err != nil {
			t.Fatalf("update() failed for drink %s in file %s", key, kvdbfile)
		}

		var d3 drink
		if err = drinksTbl.get(key, &d3); err != nil {
			t.Fatalf("get() failed for drink %s in file %s", key, kvdbfile)
		}
		if !reflect.DeepEqual(d3, d2) {
			t.Fatalf("got wrong new data for drink %s in file %s: %#v instead of %#v", key, kvdbfile, d3, d2)
		}
	}

	db.flush()
}

func TestKVDBSetAgain(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, false)

	for _, uu := range users {
		key := uu.Username
		u2 := uu
		u2.Username += "-New"

		if err := usersTbl.set(key, &u2); err != nil {
			t.Fatalf("set() again failed for user %s in file %s", key, kvdbfile)
		}

		var u3 user
		if err := usersTbl.get(key, &u3); err != nil {
			t.Fatalf("get() failed for user %s in file %s", key, kvdbfile)
		}
		if !reflect.DeepEqual(u3, u2) {
			t.Fatalf("got wrong new data for user %s in file %s: %#v instead of %#v", key, kvdbfile, u3, u2)
		}
	}

	for _, dd := range drinks {
		key := dd.Name
		d2 := dd
		d2.Alcohol = !d2.Alcohol

		if err := drinksTbl.update(key, &d2); err != nil {
			t.Fatalf("set() again failed for drink %s in file %s", key, kvdbfile)
		}

		var d3 drink
		if err := drinksTbl.get(key, &d3); err != nil {
			t.Fatalf("get() failed for drink %s in file %s", key, kvdbfile)
		}
		if !reflect.DeepEqual(d3, d2) {
			t.Fatalf("got wrong new data for drink %s in file %s: %#v instead of %#v", key, kvdbfile, d3, d2)
		}
	}

	db.flush()
}

func TestKVDBDelete(t *testing.T) {
	kvdbfile, db, usersTbl, drinksTbl := createTestDB(t)
	defer os.Remove(kvdbfile)
	defer db.close()

	initTestTables(t, usersTbl, drinksTbl, false)

	db.flush()

	// Delete entries and verify that they no longer exist.

	for _, uu := range users {
		key := uu.Username
		if err := usersTbl.del(key); err != nil {
			t.Errorf("del() failed for user %s in file %s", key, kvdbfile)
		}
		if usersTbl.hasKey(key) {
			t.Errorf("hasKey() still finds deleted user %s in file %s", key, kvdbfile)
		}
	}

	for _, dd := range drinks {
		key := dd.Name
		if err := drinksTbl.del(key); err != nil {
			t.Errorf("del() failed for drink %s in file %s", key, kvdbfile)
		}
		if drinksTbl.hasKey(key) {
			t.Errorf("hasKey() still finds deleted drink %s in file %s", key, kvdbfile)
		}
	}

	db.flush()
}
