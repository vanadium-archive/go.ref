// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"errors"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

// transaction is a wrapper around LevelDB WriteBatch that implements
// the store.Transaction interface.
type transaction struct {
	mu       sync.Mutex
	d        *db
	snapshot store.Snapshot
	batch    *C.leveldb_writebatch_t
	cOpts    *C.leveldb_writeoptions_t
	err      error
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(d *db) *transaction {
	return &transaction{
		d:        d,
		snapshot: d.NewSnapshot(),
		batch:    C.leveldb_writebatch_create(),
		cOpts:    d.writeOptions,
	}
}

// close frees allocated C objects and releases acquired locks.
func (tx *transaction) close() {
	tx.d.txmu.Unlock()
	C.leveldb_writebatch_destroy(tx.batch)
	tx.batch = nil
	if tx.cOpts != tx.d.writeOptions {
		C.leveldb_writeoptions_destroy(tx.cOpts)
	}
	tx.cOpts = nil
}

// ResetForRetry implements the store.Transaction interface.
func (tx *transaction) ResetForRetry() {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.batch == nil {
		panic(tx.err)
	}
	tx.snapshot.Close()
	tx.snapshot = tx.d.NewSnapshot()
	tx.err = nil
	C.leveldb_writebatch_clear(tx.batch)
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return valbuf, tx.err
	}
	return tx.snapshot.Get(key, valbuf)
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return &store.InvalidStream{tx.err}
	}
	return tx.snapshot.Scan(start, limit)
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return tx.err
	}
	cKey, cKeyLen := cSlice(key)
	cVal, cValLen := cSlice(value)
	C.leveldb_writebatch_put(tx.batch, cKey, cKeyLen, cVal, cValLen)
	return nil
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return tx.err
	}
	cKey, cKeyLen := cSlice(key)
	C.leveldb_writebatch_delete(tx.batch, cKey, cKeyLen)
	return nil
}

// Commit implements the store.Transaction interface.
func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return tx.err
	}
	tx.d.mu.Lock()
	defer tx.d.mu.Unlock()
	var cError *C.char
	C.leveldb_write(tx.d.cDb, tx.cOpts, tx.batch, &cError)
	if err := goError(cError); err != nil {
		tx.err = errors.New("already attempted to commit transaction")
		return err
	}
	tx.err = errors.New("committed transaction")
	tx.close()
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.batch == nil {
		return tx.err
	}
	tx.err = errors.New("aborted transaction")
	tx.close()
	return nil
}
