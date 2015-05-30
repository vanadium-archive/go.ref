// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"container/list"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

type scanRange struct {
	start, limit []byte
}

type readSet struct {
	keys   [][]byte
	ranges []scanRange
}

type writeType int

const (
	putOp writeType = iota
	deleteOp
)

type writeOp struct {
	t     writeType
	key   []byte
	value []byte
}

// commitedTransaction is only used as an element of db.txEvents.
type commitedTransaction struct {
	seq   uint64
	batch [][]byte
}

// transaction is a wrapper around LevelDB WriteBatch that implements
// the store.Transaction interface.
type transaction struct {
	// mu protects the state of the transaction.
	mu       sync.Mutex
	node     *store.ResourceNode
	d        *db
	seq      uint64
	event    *list.Element // pointer to element of db.txEvents
	snapshot store.Snapshot
	reads    readSet
	writes   []writeOp
	cOpts    *C.leveldb_writeoptions_t
	err      error
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(d *db, parent *store.ResourceNode) *transaction {
	node := store.NewResourceNode()
	snapshot := newSnapshot(d, node)
	tx := &transaction{
		node:     node,
		d:        d,
		snapshot: snapshot,
		seq:      d.txSequenceNumber,
		cOpts:    d.writeOptions,
	}
	tx.event = d.txEvents.PushFront(tx)
	parent.AddChild(tx.node, func() {
		tx.Abort()
	})
	return tx
}

// close frees allocated C objects and releases acquired locks.
// Assumes mu is held.
func (tx *transaction) close() {
	tx.removeEvent()
	tx.node.Close()
	if tx.cOpts != tx.d.writeOptions {
		C.leveldb_writeoptions_destroy(tx.cOpts)
	}
	tx.cOpts = nil
}

// removeEvent removes this transaction from the db.txEvents queue.
// Assumes mu is held.
func (tx *transaction) removeEvent() {
	// This can happen if the transaction was committed, since Commit()
	// explicitly calls removeEvent().
	if tx.event == nil {
		return
	}
	tx.d.txmu.Lock()
	tx.d.txEvents.Remove(tx.event)
	tx.d.txmu.Unlock()
	tx.event = nil
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return valbuf, store.WrapError(tx.err)
	}
	tx.reads.keys = append(tx.reads.keys, key)
	return tx.snapshot.Get(key, valbuf)
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return &store.InvalidStream{tx.err}
	}
	tx.reads.ranges = append(tx.reads.ranges, scanRange{
		start: start,
		limit: limit,
	})
	return tx.snapshot.Scan(start, limit)
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	tx.writes = append(tx.writes, writeOp{
		t:     putOp,
		key:   key,
		value: value,
	})
	return nil
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	tx.writes = append(tx.writes, writeOp{
		t:   deleteOp,
		key: key,
	})
	return nil
}

// validateReadSet returns true iff the read set of this transaction has not
// been invalidated by other transactions.
// Assumes tx.d.txmu is held.
func (tx *transaction) validateReadSet() bool {
	for _, key := range tx.reads.keys {
		if tx.d.txTable.get(key) > tx.seq {
			return false
		}
	}
	for _, r := range tx.reads.ranges {
		if tx.d.txTable.rangeMax(r.start, r.limit) > tx.seq {
			return false
		}

	}
	return true
}

// Commit implements the store.Transaction interface.
func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	// Explicitly remove this transaction from the event queue. If this was the
	// only active transaction, the event queue becomes empty and writeLocked will
	// not add the write set of this transaction to the txTable.
	tx.removeEvent()
	defer tx.close()
	tx.d.txmu.Lock()
	defer tx.d.txmu.Unlock()
	if !tx.validateReadSet() {
		return store.NewErrConcurrentTransaction(nil)
	}
	if err := tx.d.writeLocked(tx.writes, tx.cOpts); err != nil {
		// Once Commit() has failed, subsequent ops on the transaction will fail
		// with the following error.
		tx.err = verror.New(verror.ErrBadState, nil, "already attempted to commit transaction")
		return err
	}
	tx.err = verror.New(verror.ErrBadState, nil, "committed transaction")
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
