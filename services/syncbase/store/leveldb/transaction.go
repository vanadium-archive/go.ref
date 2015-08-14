// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"bytes"
	"container/list"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

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
	reads    store.ReadSet
	writes   []store.WriteOp
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
		return valbuf, convertError(tx.err)
	}
	tx.reads.Keys = append(tx.reads.Keys, key)

	// Reflect the state of the transaction: the "writes" (puts and
	// deletes) override the values in the transaction snapshot.
	// Find the last "writes" entry for this key, if one exists.
	// Note: this step could be optimized by using maps (puts and
	// deletes) instead of an array.
	for i := len(tx.writes) - 1; i >= 0; i-- {
		op := &tx.writes[i]
		if bytes.Equal(op.Key, key) {
			if op.T == store.PutOp {
				return op.Value, nil
			}
			return valbuf, verror.New(store.ErrUnknownKey, nil, string(key))
		}
	}

	return tx.snapshot.Get(key, valbuf)
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return &store.InvalidStream{Error: tx.err}
	}

	tx.reads.Ranges = append(tx.reads.Ranges, store.ScanRange{
		Start: start,
		Limit: limit,
	})

	// Return a stream which merges the snaphot stream with the uncommitted changes.
	return store.MergeWritesWithStream(tx.snapshot, tx.writes, start, limit)
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return convertError(tx.err)
	}
	tx.writes = append(tx.writes, store.WriteOp{
		T:     store.PutOp,
		Key:   key,
		Value: value,
	})
	return nil
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return convertError(tx.err)
	}
	tx.writes = append(tx.writes, store.WriteOp{
		T:   store.DeleteOp,
		Key: key,
	})
	return nil
}

// validateReadSet returns true iff the read set of this transaction has not
// been invalidated by other transactions.
// Assumes tx.d.txmu is held.
func (tx *transaction) validateReadSet() bool {
	for _, key := range tx.reads.Keys {
		if tx.d.txTable.get(key) > tx.seq {
			vlog.VI(3).Infof("key conflict: %q", key)
			return false
		}
	}
	for _, r := range tx.reads.Ranges {
		if tx.d.txTable.rangeMax(r.Start, r.Limit) > tx.seq {
			vlog.VI(3).Infof("range conflict: {%q, %q}", r.Start, r.Limit)
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
		return convertError(tx.err)
	}
	tx.err = verror.New(verror.ErrBadState, nil, store.ErrMsgCommittedTxn)
	// Explicitly remove this transaction from the event queue. If this was the
	// only active transaction, the event queue becomes empty and writeLocked will
	// not add this transaction's write set to txTable.
	tx.removeEvent()
	defer tx.close()
	tx.d.txmu.Lock()
	defer tx.d.txmu.Unlock()
	if !tx.validateReadSet() {
		return store.NewErrConcurrentTransaction(nil)
	}
	return tx.d.writeLocked(tx.writes, tx.cOpts)
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return convertError(tx.err)
	}
	tx.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgAbortedTxn)
	tx.close()
	return nil
}
