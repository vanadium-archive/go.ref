// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package leveldb provides a LevelDB-based implementation of store.Store.
package leveldb

// #cgo LDFLAGS: -lleveldb -lsnappy
// #include <stdlib.h>
// #include "leveldb/c.h"
// #include "syncbase_leveldb.h"
import "C"
import (
	"errors"
	"sync"
	"unsafe"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

var (
	errClosedStore = errors.New("closed store")
)

// db is a wrapper around LevelDB that implements the store.Store interface.
type db struct {
	// mu protects the state of the db.
	mu   sync.RWMutex
	node *resourceNode
	cDb  *C.leveldb_t
	// Default read/write options.
	readOptions  *C.leveldb_readoptions_t
	writeOptions *C.leveldb_writeoptions_t
	err          error
	// Used to prevent concurrent transactions.
	// TODO(rogulenko): improve concurrency.
	txmu sync.Mutex
}

var _ store.Store = (*db)(nil)

// Open opens the database located at the given path, creating it if it doesn't
// exist.
func Open(path string) (store.Store, error) {
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
	return &db{
		node:         newResourceNode(),
		cDb:          cDb,
		readOptions:  readOptions,
		writeOptions: C.leveldb_writeoptions_create(),
	}, nil
}

// Close implements the store.Store interface.
func (d *db) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	d.node.close()
	C.leveldb_close(d.cDb)
	d.cDb = nil
	C.leveldb_readoptions_destroy(d.readOptions)
	d.readOptions = nil
	C.leveldb_writeoptions_destroy(d.writeOptions)
	d.writeOptions = nil
	d.err = errors.New("closed store")
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

// Get implements the store.StoreReader interface.
func (d *db) Get(key, valbuf []byte) ([]byte, error) {
	return d.getWithOpts(key, valbuf, d.readOptions)
}

// Scan implements the store.StoreReader interface.
func (d *db) Scan(start, limit []byte) store.Stream {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return &store.InvalidStream{d.err}
	}
	return newStream(d, d.node, start, limit, d.readOptions)
}

// Put implements the store.StoreWriter interface.
func (d *db) Put(key, value []byte) error {
	// TODO(rogulenko): improve performance.
	return store.RunInTransaction(d, func(st store.StoreReadWriter) error {
		return st.Put(key, value)
	})
}

// Delete implements the store.StoreWriter interface.
func (d *db) Delete(key []byte) error {
	// TODO(rogulenko): improve performance.
	return store.RunInTransaction(d, func(st store.StoreReadWriter) error {
		return st.Delete(key)
	})
}

// NewTransaction implements the store.Store interface.
func (d *db) NewTransaction() store.Transaction {
	// txmu is held until the transaction is successfully committed or aborted.
	d.txmu.Lock()
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		d.txmu.Unlock()
		return &store.InvalidTransaction{d.err}
	}
	return newTransaction(d, d.node)
}

// NewSnapshot implements the store.Store interface.
func (d *db) NewSnapshot() store.Snapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return &store.InvalidSnapshot{d.err}
	}
	return newSnapshot(d, d.node)
}

// getWithOpts returns the value for the given key.
// cOpts may contain a pointer to a snapshot.
func (d *db) getWithOpts(key, valbuf []byte, cOpts *C.leveldb_readoptions_t) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return valbuf, d.err
	}
	var cError *C.char
	var valLen C.size_t
	cStr, cLen := cSlice(key)
	val := C.leveldb_get(d.cDb, cOpts, cStr, cLen, &valLen, &cError)
	if err := goError(cError); err != nil {
		return valbuf, err
	}
	if val == nil {
		return valbuf, &store.ErrUnknownKey{Key: string(key)}
	}
	defer C.leveldb_free(unsafe.Pointer(val))
	return store.CopyBytes(valbuf, goBytes(val, valLen)), nil
}
