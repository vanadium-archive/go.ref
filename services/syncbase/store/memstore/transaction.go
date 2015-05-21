// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"errors"
	"sync"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

var (
	txnTimeout         = time.Duration(5) * time.Second
	errExpiredTxn      = errors.New("expired transaction")
	errCommittedTxn    = errors.New("committed transaction")
	errAbortedTxn      = errors.New("aborted transaction")
	errAttemptedCommit = errors.New("already attempted to commit transaction")
)

type transaction struct {
	mu sync.Mutex
	st *memstore
	sn *snapshot
	// The following fields are used to determine whether method calls should
	// error out.
	err         error
	seq         uint64
	createdTime time.Time
	// The following fields track writes performed against this transaction.
	puts    map[string][]byte
	deletes map[string]struct{}
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(st *memstore, seq uint64) *transaction {
	return &transaction{
		st:          st,
		sn:          newSnapshot(st),
		seq:         seq,
		createdTime: time.Now(),
		puts:        map[string][]byte{},
		deletes:     map[string]struct{}{},
	}
}

func (tx *transaction) expired() bool {
	return time.Now().After(tx.createdTime.Add(txnTimeout))
}

func (tx *transaction) error() error {
	if tx.err != nil {
		return tx.err
	}
	if tx.expired() {
		return errExpiredTxn
	}
	return nil
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if err := tx.error(); err != nil {
		return valbuf, err
	}
	return tx.sn.Get(key, valbuf)
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if err := tx.error(); err != nil {
		return &store.InvalidStream{err}
	}
	return newStream(tx.sn, start, limit)
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.error(); err != nil {
		return err
	}
	delete(tx.deletes, string(key))
	tx.puts[string(key)] = value
	return nil
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.error(); err != nil {
		return err
	}
	delete(tx.puts, string(key))
	tx.deletes[string(key)] = struct{}{}
	return nil
}

// Commit implements the store.Transaction interface.
func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if err := tx.error(); err != nil {
		return err
	}
	tx.sn.Close()
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock() // note, defer is last-in-first-out
	if tx.seq <= tx.st.lastCommitSeq {
		// Once Commit() has failed with store.ErrConcurrentTransaction, subsequent
		// ops on the transaction will fail with errAttemptedCommit.
		tx.err = errAttemptedCommit
		return &store.ErrConcurrentTransaction{}
	}
	tx.err = errCommittedTxn
	for k, v := range tx.puts {
		tx.st.data[k] = v
	}
	for k := range tx.deletes {
		delete(tx.st.data, k)
	}
	tx.st.lastCommitSeq = tx.st.lastSeq
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if err := tx.error(); err != nil {
		return err
	}
	tx.sn.Close()
	tx.err = errAbortedTxn
	return nil
}
