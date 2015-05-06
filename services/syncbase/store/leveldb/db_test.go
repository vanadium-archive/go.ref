// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

import (
	"fmt"
	"io/ioutil"
	"runtime"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store/test"
)

func init() {
	runtime.GOMAXPROCS(10)
}

func TestReadWriteBasic(t *testing.T) {
	db, dbPath := newDB()
	defer destroyDB(db, dbPath)
	test.RunReadWriteBasicTest(t, db)
}

func TestReadWriteRandom(t *testing.T) {
	db, dbPath := newDB()
	defer destroyDB(db, dbPath)
	test.RunReadWriteRandomTest(t, db)
}

func TestTransactionsWithGet(t *testing.T) {
	db, dbPath := newDB()
	defer destroyDB(db, dbPath)
	test.RunTransactionsWithGetTest(t, db)
}

func newDB() (*DB, string) {
	dbPath, err := ioutil.TempDir("", "syncbase_leveldb")
	if err != nil {
		panic(fmt.Sprintf("can't create temp dir: %v", err))
	}
	db, err := Open(dbPath)
	if err != nil {
		panic(fmt.Sprintf("can't open db in %v: %v", dbPath, err))
	}
	return db, dbPath
}

func destroyDB(db *DB, dbPath string) {
	db.Close()
	if err := Destroy(dbPath); err != nil {
		panic(fmt.Sprintf("can't destroy db located in %v: %v", dbPath, err))
	}
}
