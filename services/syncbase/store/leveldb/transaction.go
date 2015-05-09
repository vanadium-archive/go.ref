// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"v.io/syncbase/x/ref/services/syncbase/store"
)

// transaction is a wrapper around LevelDB WriteBatch that implements
// the store.Transaction interface.
// TODO(rogulenko): handle incorrect usage.
type transaction struct {
	d        *db
	snapshot store.Snapshot
	batch    *C.leveldb_writebatch_t
	cOpts    *C.leveldb_writeoptions_t
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(d *db) *transaction {
	// The lock is held until the transaction is successfully
	// committed or aborted.
	d.mu.Lock()
	return &transaction{
		d,
		d.NewSnapshot(),
		C.leveldb_writebatch_create(),
		d.writeOptions,
	}
}

// close frees allocated C objects and releases acquired locks.
func (tx *transaction) close() {
	tx.d.mu.Unlock()
	tx.snapshot.Close()
	C.leveldb_writebatch_destroy(tx.batch)
	if tx.cOpts != tx.d.writeOptions {
		C.leveldb_writeoptions_destroy(tx.cOpts)
	}
}

// ResetForRetry implements the store.Transaction interface.
func (tx *transaction) ResetForRetry() {
	tx.snapshot.Close()
	tx.snapshot = tx.d.NewSnapshot()
	C.leveldb_writebatch_clear(tx.batch)
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	return tx.snapshot.Get(key, valbuf)
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, end []byte) (store.Stream, error) {
	return tx.snapshot.Scan(start, end)
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	cKey, cKeyLen := cSlice(key)
	cVal, cValLen := cSlice(value)
	C.leveldb_writebatch_put(tx.batch, cKey, cKeyLen, cVal, cValLen)
	return nil
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	cKey, cKeyLen := cSlice(key)
	C.leveldb_writebatch_delete(tx.batch, cKey, cKeyLen)
	return nil
}

// Commit implements the store.Transaction interface.
func (tx *transaction) Commit() error {
	var cError *C.char
	C.leveldb_write(tx.d.cDb, tx.cOpts, tx.batch, &cError)
	if err := goError(cError); err != nil {
		return err
	}
	tx.close()
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.close()
	return nil
}
