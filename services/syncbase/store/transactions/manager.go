// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transactions

import (
	"container/list"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

// BatchStore is a CRUD-capable storage engine that supports atomic batch
// writes. BatchStore doesn't support transactions.
// This interface is a Go version of the C++ LevelDB interface. It serves as
// an intermediate interface between store.Store and the LevelDB interface.
type BatchStore interface {
	store.StoreReader

	// WriteBatch atomically writes a list of write operations to the database.
	WriteBatch(batch ...WriteOp) error

	// Close closes the store.
	Close() error

	// NewSnapshot creates a snapshot.
	NewSnapshot() store.Snapshot
}

// manager handles transaction-related operations of the store.
type manager struct {
	BatchStore
	// mu protects the variables below, and is also held during transaction
	// commits. It must always be acquired before the store-level lock.
	mu sync.Mutex
	// events is a queue of create/commit transaction events.
	events *list.List
	seq    uint64
	// txTable is a set of keys written by recent transactions. This set
	// includes all write sets of transactions committed after the oldest living
	// (in-flight) transaction.
	txTable *trie
}

// commitedTransaction is only used as an element of manager.events.
type commitedTransaction struct {
	seq   uint64
	batch [][]byte
}

// Wrap wraps the BatchStore with transaction functionality.
func Wrap(bs BatchStore) store.Store {
	return &manager{
		BatchStore: bs,
		events:     list.New(),
		txTable:    newTrie(),
	}
}

// Close implements the store.Store interface.
func (mg *manager) Close() error {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	if mg.txTable == nil {
		return verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore)
	}
	mg.BatchStore.Close()
	for event := mg.events.Front(); event != nil; event = event.Next() {
		if tx, ok := event.Value.(*transaction); ok {
			// tx.Abort() internally removes tx from the mg.events list under
			// the mg.mu lock. To brake the cyclic dependency, we set tx.event
			// to nil.
			tx.mu.Lock()
			tx.event = nil
			tx.mu.Unlock()
			tx.Abort()
		}
	}
	mg.events = nil
	mg.txTable = nil
	return nil
}

// NewTransaction implements the store.Store interface.
func (mg *manager) NewTransaction() store.Transaction {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	if mg.txTable == nil {
		return &store.InvalidTransaction{
			Error: verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore),
		}
	}
	return newTransaction(mg)
}

// Put implements the store.StoreWriter interface.
func (mg *manager) Put(key, value []byte) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	if mg.txTable == nil {
		return verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore)
	}
	write := WriteOp{
		T:     PutOp,
		Key:   key,
		Value: value,
	}
	if err := mg.BatchStore.WriteBatch(write); err != nil {
		return err
	}
	mg.trackBatch(write)
	return nil
}

// Delete implements the store.StoreWriter interface.
func (mg *manager) Delete(key []byte) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()
	if mg.txTable == nil {
		return verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore)
	}
	write := WriteOp{
		T:   DeleteOp,
		Key: key,
	}
	if err := mg.BatchStore.WriteBatch(write); err != nil {
		return err
	}
	mg.trackBatch(write)
	return nil
}

// trackBatch writes the batch to txTable and adds a commit event to
// the events queue.
// Assumes mu is held.
func (mg *manager) trackBatch(batch ...WriteOp) {
	if mg.events.Len() == 0 {
		return
	}
	// TODO(rogulenko): do GC.
	mg.seq++
	var keys [][]byte
	for _, write := range batch {
		mg.txTable.add(write.Key, mg.seq)
		keys = append(keys, write.Key)
	}
	tx := &commitedTransaction{
		seq:   mg.seq,
		batch: keys,
	}
	mg.events.PushBack(tx)
}

//////////////////////////////////////////////////////////////
// Read and Write types used for storing transcation reads
// and uncommitted writes.

type WriteType int

const (
	PutOp WriteType = iota
	DeleteOp
)

type WriteOp struct {
	T     WriteType
	Key   []byte
	Value []byte
}

type scanRange struct {
	Start, Limit []byte
}

type readSet struct {
	Keys   [][]byte
	Ranges []scanRange
}

type writeOpArray []WriteOp

func (a writeOpArray) Len() int {
	return len(a)
}

func (a writeOpArray) Less(i, j int) bool {
	return string(a[i].Key) < string(a[j].Key)
}

func (a writeOpArray) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
