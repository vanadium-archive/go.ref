// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transactions

import (
	"bytes"
	"container/list"
	"sync"

	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/store"
)

// transaction is a wrapper on top of a BatchWriter and a store.Snapshot that
// implements the store.Transaction interface.
type transaction struct {
	// mu protects the state of the transaction.
	mu       sync.Mutex
	mg       *manager
	seq      uint64
	event    *list.Element // pointer to element of db.txEvents
	snapshot store.Snapshot
	reads    readSet
	writes   []WriteOp
	err      error
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(mg *manager) *transaction {
	tx := &transaction{
		mg:       mg,
		snapshot: mg.BatchStore.NewSnapshot(),
		seq:      mg.seq,
	}
	tx.event = mg.events.PushFront(tx)
	return tx
}

// close removes this transaction from the mg.events queue and aborts
// the underlying snapshot.
// Assumes mu is held.
func (tx *transaction) close() {
	tx.removeEvent()
	tx.snapshot.Abort()
}

// removeEvent removes this transaction from the mg.events queue.
// Assumes mu is held.
func (tx *transaction) removeEvent() {
	// This can happen if the transaction was committed, since Commit()
	// explicitly calls removeEvent().
	if tx.event == nil {
		return
	}
	tx.mg.mu.Lock()
	tx.mg.events.Remove(tx.event)
	tx.mg.mu.Unlock()
	tx.event = nil
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return valbuf, store.ConvertError(tx.err)
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
			if op.T == PutOp {
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

	tx.reads.Ranges = append(tx.reads.Ranges, scanRange{
		Start: start,
		Limit: limit,
	})

	// Return a stream which merges the snaphot stream with the uncommitted changes.
	return mergeWritesWithStream(tx.snapshot, tx.writes, start, limit)
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.ConvertError(tx.err)
	}
	tx.writes = append(tx.writes, WriteOp{
		T:     PutOp,
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
		return store.ConvertError(tx.err)
	}
	tx.writes = append(tx.writes, WriteOp{
		T:   DeleteOp,
		Key: key,
	})
	return nil
}

// validateReadSet returns true iff the read set of this transaction has not
// been invalidated by other transactions.
// Assumes tx.mg.mu is held.
func (tx *transaction) validateReadSet() bool {
	for _, key := range tx.reads.Keys {
		if tx.mg.txTable.get(key) > tx.seq {
			vlog.VI(3).Infof("key conflict: %q", key)
			return false
		}
	}
	for _, r := range tx.reads.Ranges {
		if tx.mg.txTable.rangeMax(r.Start, r.Limit) > tx.seq {
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
		return store.ConvertError(tx.err)
	}
	tx.err = verror.New(verror.ErrBadState, nil, store.ErrMsgCommittedTxn)
	// Explicitly remove this transaction from the event queue. If this was the
	// only active transaction, the event queue becomes empty and writeLocked will
	// not add this transaction's write set to txTable.
	tx.removeEvent()
	defer tx.close()
	tx.mg.mu.Lock()
	defer tx.mg.mu.Unlock()
	if !tx.validateReadSet() {
		return store.NewErrConcurrentTransaction(nil)
	}
	if err := tx.mg.BatchStore.WriteBatch(tx.writes...); err != nil {
		return err
	}
	tx.mg.trackBatch(tx.writes...)
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.ConvertError(tx.err)
	}
	tx.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgAbortedTxn)
	tx.close()
	return nil
}
