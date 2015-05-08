// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #cgo LDFLAGS: -lleveldb
// #include <stdlib.h>
// #include "leveldb/c.h"
// #include "syncbase_leveldb.h"
import "C"
import (
	"sync"
	"unsafe"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

// DB is a wrapper around LevelDB that implements the store.Store interface.
// TODO(rogulenko): ensure thread safety.
type DB struct {
	cDb *C.leveldb_t
	// Default read/write options.
	readOptions  *C.leveldb_readoptions_t
	writeOptions *C.leveldb_writeoptions_t
	// Used to prevent concurrent transactions.
	// TODO(rogulenko): improve concurrency.
	mu sync.Mutex
}

var _ store.Store = (*DB)(nil)

// Open opens the database located at the given path, creating it if it doesn't
// exist.
func Open(path string) (*DB, error) {
	var cError *C.char
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	cOpts := C.leveldb_options_create()
	C.leveldb_options_set_create_if_missing(cOpts, 1)
	C.leveldb_options_set_paranoid_checks(cOpts, 1)
	defer C.leveldb_options_destroy(cOpts)

	cDb := C.leveldb_open(cOpts, cPath, &cError)
	if err := goError(cError); err != nil {
		return nil, err
	}
	readOptions := C.leveldb_readoptions_create()
	C.leveldb_readoptions_set_verify_checksums(readOptions, 1)
	return &DB{
		cDb:          cDb,
		readOptions:  readOptions,
		writeOptions: C.leveldb_writeoptions_create(),
	}, nil
}

// Close implements the store.Store interface.
func (db *DB) Close() error {
	C.leveldb_close(db.cDb)
	C.leveldb_readoptions_destroy(db.readOptions)
	C.leveldb_writeoptions_destroy(db.writeOptions)
	return nil
}

// Destroy removes all physical data of the database located at the given path.
func Destroy(path string) error {
	var cError *C.char
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cOpts := C.leveldb_options_create()
	defer C.leveldb_options_destroy(cOpts)
	C.leveldb_destroy_db(cOpts, cPath, &cError)
	return goError(cError)
}

// NewTransaction implements the store.Store interface.
func (db *DB) NewTransaction() store.Transaction {
	return newTransaction(db)
}

// NewSnapshot implements the store.Store interface.
func (db *DB) NewSnapshot() store.Snapshot {
	return newSnapshot(db)
}

// Scan implements the store.StoreReader interface.
func (db *DB) Scan(start, end []byte) (store.Stream, error) {
	return newStream(db, start, end, db.readOptions), nil
}

// Get implements the store.StoreReader interface.
func (db *DB) Get(key, valbuf []byte) ([]byte, error) {
	return db.getWithOpts(key, valbuf, db.readOptions)
}

// Put implements the store.StoreWriter interface.
func (db *DB) Put(key, value []byte) error {
	// TODO(rogulenko): improve performance.
	return store.RunInTransaction(db, func(st store.StoreReadWriter) error {
		return st.Put(key, value)
	})
}

// Delete implements the store.StoreWriter interface.
func (db *DB) Delete(key []byte) error {
	// TODO(rogulenko): improve performance.
	return store.RunInTransaction(db, func(st store.StoreReadWriter) error {
		return st.Delete(key)
	})
}

// getWithOpts returns the value for the given key.
// cOpts may contain a pointer to a snapshot.
func (db *DB) getWithOpts(key, valbuf []byte, cOpts *C.leveldb_readoptions_t) ([]byte, error) {
	var cError *C.char
	var valLen C.size_t
	cStr, cLen := cSlice(key)
	val := C.leveldb_get(db.cDb, cOpts, cStr, cLen, &valLen, &cError)
	if err := goError(cError); err != nil {
		return nil, err
	}
	if val == nil {
		return nil, &store.ErrUnknownKey{Key: string(key)}
	}
	defer C.leveldb_free(unsafe.Pointer(val))
	return copyAll(valbuf, goBytes(val, valLen)), nil
}
