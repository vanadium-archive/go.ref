// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

import (
	"fmt"
	"io/ioutil"
	"runtime"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/test"
)

func init() {
	runtime.GOMAXPROCS(10)
}

func TestStream(t *testing.T) {
	db, dbPath := newDB()
	defer destroyDB(db, dbPath)
	test.RunStreamTest(t, db)
}

func TestReadWriteBasic(t *testing.T) {
	st, path := newDB()
	defer destroyDB(st, path)
	test.RunReadWriteBasicTest(t, st)
}

func TestReadWriteRandom(t *testing.T) {
	st, path := newDB()
	defer destroyDB(st, path)
	test.RunReadWriteRandomTest(t, st)
}

func TestTransactionsWithGet(t *testing.T) {
	st, path := newDB()
	defer destroyDB(st, path)
	test.RunTransactionsWithGetTest(t, st)
}

func newDB() (store.Store, string) {
	path, err := ioutil.TempDir("", "syncbase_leveldb")
	if err != nil {
		panic(fmt.Sprintf("can't create temp dir: %v", err))
	}
	st, err := Open(path)
	if err != nil {
		panic(fmt.Sprintf("can't open db at %v: %v", path, err))
	}
	return st, path
}

func destroyDB(st store.Store, path string) {
	st.Close()
	if err := Destroy(path); err != nil {
		panic(fmt.Sprintf("can't destroy db at %v: %v", path, err))
	}
}
