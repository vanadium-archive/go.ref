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
	"container/list"
	"fmt"
	"sync"
	"unsafe"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

// db is a wrapper around LevelDB that implements the store.Store interface.
type db struct {
	// mu protects the state of the db.
	mu   sync.RWMutex
	node *store.ResourceNode
	cDb  *C.leveldb_t
	// Default read/write options.
	readOptions  *C.leveldb_readoptions_t
	writeOptions *C.leveldb_writeoptions_t
	err          error

	// TODO(rogulenko): decide whether we need to make a defensive copy of
	// keys/values used by transactions.
	// txmu protects transaction-related variables below. It is also held during
	// commit.
	// txmu must always be acquired before mu.
	txmu sync.Mutex
	// txEvents is a queue of create/commit transaction events.
	txEvents         *list.List
	txSequenceNumber uint64
	// txTable is a set of keys written by recent transactions. This set
	// includes all write sets of transactions committed after the oldest living
	// transaction.
	txTable *trie
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
		node:         store.NewResourceNode(),
		cDb:          cDb,
		readOptions:  readOptions,
		writeOptions: C.leveldb_writeoptions_create(),
		txEvents:     list.New(),
		txTable:      newTrie(),
	}, nil
}

// Close implements the store.Store interface.
func (d *db) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return store.WrapError(d.err)
	}
	d.node.Close()
	C.leveldb_close(d.cDb)
	d.cDb = nil
	C.leveldb_readoptions_destroy(d.readOptions)
	d.readOptions = nil
	C.leveldb_writeoptions_destroy(d.writeOptions)
	d.writeOptions = nil
	d.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore)
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
	write := writeOp{
		t:     putOp,
		key:   key,
		value: value,
	}
	return d.write([]writeOp{write}, d.writeOptions)
}

// Delete implements the store.StoreWriter interface.
func (d *db) Delete(key []byte) error {
	write := writeOp{
		t:   deleteOp,
		key: key,
	}
	return d.write([]writeOp{write}, d.writeOptions)
}

// NewTransaction implements the store.Store interface.
func (d *db) NewTransaction() store.Transaction {
	d.txmu.Lock()
	defer d.txmu.Unlock()
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
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

// write writes a batch and adds all written keys to txTable.
// TODO(rogulenko): remove this method.
func (d *db) write(batch []writeOp, cOpts *C.leveldb_writeoptions_t) error {
	d.txmu.Lock()
	defer d.txmu.Unlock()
	return d.writeLocked(batch, cOpts)
}

// writeLocked is like write(), but it assumes txmu is held.
func (d *db) writeLocked(batch []writeOp, cOpts *C.leveldb_writeoptions_t) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	cBatch := C.leveldb_writebatch_create()
	defer C.leveldb_writebatch_destroy(cBatch)
	for _, write := range batch {
		switch write.t {
		case putOp:
			cKey, cKeyLen := cSlice(write.key)
			cVal, cValLen := cSlice(write.value)
			C.leveldb_writebatch_put(cBatch, cKey, cKeyLen, cVal, cValLen)
		case deleteOp:
			cKey, cKeyLen := cSlice(write.key)
			C.leveldb_writebatch_delete(cBatch, cKey, cKeyLen)
		default:
			panic(fmt.Sprintf("unknown write operation type: %v", write.t))
		}
	}
	var cError *C.char
	C.leveldb_write(d.cDb, cOpts, cBatch, &cError)
	if err := goError(cError); err != nil {
		return err
	}
	if d.txEvents.Len() == 0 {
		return nil
	}
	d.trackBatch(batch)
	return nil
}

// trackBatch writes the batch to txTable and adds a commit event to txEvents.
func (d *db) trackBatch(batch []writeOp) {
	// TODO(rogulenko): do GC.
	d.txSequenceNumber++
	seq := d.txSequenceNumber
	var keys [][]byte
	for _, write := range batch {
		d.txTable.add(write.key, seq)
		keys = append(keys, write.key)
	}
	tx := &commitedTransaction{
		seq:   seq,
		batch: keys,
	}
	d.txEvents.PushBack(tx)
}

// getWithOpts returns the value for the given key.
// cOpts may contain a pointer to a snapshot.
func (d *db) getWithOpts(key, valbuf []byte, cOpts *C.leveldb_readoptions_t) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return valbuf, store.WrapError(d.err)
	}
	var cError *C.char
	var valLen C.size_t
	cStr, cLen := cSlice(key)
	val := C.leveldb_get(d.cDb, cOpts, cStr, cLen, &valLen, &cError)
	if err := goError(cError); err != nil {
		return valbuf, err
	}
	if val == nil {
		return valbuf, verror.New(store.ErrUnknownKey, nil, string(key))
	}
	defer C.leveldb_free(unsafe.Pointer(val))
	return store.CopyBytes(valbuf, goBytes(val, valLen)), nil
}
