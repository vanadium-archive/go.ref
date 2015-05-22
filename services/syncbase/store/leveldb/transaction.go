// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

// transaction is a wrapper around LevelDB WriteBatch that implements
// the store.Transaction interface.
type transaction struct {
	// mu protects the state of the transaction.
	mu       sync.Mutex
	node     *resourceNode
	d        *db
	snapshot store.Snapshot
	batch    *C.leveldb_writebatch_t
	cOpts    *C.leveldb_writeoptions_t
	err      error
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(d *db, parent *resourceNode) *transaction {
	tx := &transaction{
		node:     newResourceNode(),
		d:        d,
		snapshot: d.NewSnapshot(),
		batch:    C.leveldb_writebatch_create(),
		cOpts:    d.writeOptions,
	}
	parent.addChild(tx.node, func() {
		tx.Abort()
	})
	return tx
}

// close frees allocated C objects and releases acquired locks.
// Assumes mu is held.
func (tx *transaction) close() {
	tx.d.txmu.Unlock()
	tx.node.close()
	tx.snapshot.Close()
	C.leveldb_writebatch_destroy(tx.batch)
	tx.batch = nil
	if tx.cOpts != tx.d.writeOptions {
		C.leveldb_writeoptions_destroy(tx.cOpts)
	}
	tx.cOpts = nil
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return valbuf, store.WrapError(tx.err)
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
		return store.WrapError(tx.err)
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
		return store.WrapError(tx.err)
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
		return store.WrapError(tx.err)
	}
	tx.d.mu.Lock()
	defer tx.d.mu.Unlock()
	var cError *C.char
	C.leveldb_write(tx.d.cDb, tx.cOpts, tx.batch, &cError)
	if err := goError(cError); err != nil {
		// Once Commit() has failed with store.ErrConcurrentTransaction, subsequent
		// ops on the transaction will fail with the following error.
		tx.err = verror.New(verror.ErrBadState, nil, "already attempted to commit transaction")
		tx.close()
		return err
	}
	tx.err = verror.New(verror.ErrBadState, nil, "committed transaction")
	tx.close()
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	tx.err = verror.New(verror.ErrCanceled, nil, "aborted transaction")
	tx.close()
	return nil
}
