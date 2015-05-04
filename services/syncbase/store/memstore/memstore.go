// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memstore provides a simple, in-memory implementation of
// store.TransactableStore. Since it's a prototype implementation, it makes no
// attempt to be performant.
package memstore

import (
	"errors"
	"sync"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/x/lib/vlog"
)

var (
	txnTimeout       = time.Duration(5) * time.Second
	errExpiredTxn    = errors.New("expired transaction")
	errCommittedTxn  = errors.New("committed transaction")
	errAbortedTxn    = errors.New("aborted transaction")
	errConcurrentTxn = errors.New("concurrent transaction")
)

type transaction struct {
	st *memstore
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

type memstore struct {
	mu   sync.Mutex
	data map[string][]byte
	// Most recent sequence number handed out.
	lastSeq uint64
	// Value of lastSeq at the time of the most recent commit.
	lastCommitSeq uint64
}

var _ store.Store = (*memstore)(nil)

func New() store.Store {
	return &memstore{data: map[string][]byte{}}
}

////////////////////////////////////////
// transaction methods

func newTxn(st *memstore, seq uint64) *transaction {
	return &transaction{
		st:          st,
		seq:         seq,
		createdTime: time.Now(),
		puts:        map[string][]byte{},
		deletes:     map[string]struct{}{},
	}
}

func (tx *transaction) expired() bool {
	return time.Now().After(tx.createdTime.Add(txnTimeout))
}

func (tx *transaction) checkError() error {
	if tx.err != nil {
		return tx.err
	}
	if tx.expired() {
		return errExpiredTxn
	}
	if tx.seq <= tx.st.lastCommitSeq {
		return errConcurrentTxn
	}
	return nil
}

func (tx *transaction) Scan(start, end string) (store.Stream, error) {
	vlog.Fatal("not implemented")
	return nil, nil
}

func (tx *transaction) Get(k string) ([]byte, error) {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.checkError(); err != nil {
		return nil, err
	}
	v, ok := tx.st.data[k]
	if !ok {
		return nil, &store.ErrUnknownKey{Key: k}
	}
	return v, nil
}

func (tx *transaction) Put(k string, v []byte) error {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.checkError(); err != nil {
		return err
	}
	delete(tx.deletes, k)
	tx.puts[k] = v
	return nil
}

func (tx *transaction) Delete(k string) error {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.checkError(); err != nil {
		return err
	}
	delete(tx.puts, k)
	tx.deletes[k] = struct{}{}
	return nil
}

func (tx *transaction) Commit() error {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.checkError(); err != nil {
		return err
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

func (tx *transaction) Abort() error {
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if err := tx.checkError(); err != nil {
		return err
	}
	tx.err = errAbortedTxn
	return nil
}

func (tx *transaction) ResetForRetry() {
	tx.puts = make(map[string][]byte)
	tx.deletes = make(map[string]struct{})
	tx.err = nil
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	tx.st.lastSeq++
	tx.seq = tx.st.lastSeq
}

////////////////////////////////////////
// memstore methods

func (st *memstore) Scan(start, end string) (store.Stream, error) {
	vlog.Fatal("not implemented")
	return nil, nil
}

func (st *memstore) Get(k string) ([]byte, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	v, ok := st.data[k]
	if !ok {
		return nil, &store.ErrUnknownKey{Key: k}
	}
	return v, nil
}

func (st *memstore) Put(k string, v []byte) error {
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Put(k, v)
	})
}

func (st *memstore) Delete(k string) error {
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Delete(k)
	})
}

func (st *memstore) NewTransaction() store.Transaction {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeq++
	return newTxn(st, st.lastSeq)
}

func (st *memstore) NewSnapshot() store.Snapshot {
	vlog.Fatal("not implemented")
	return nil
}
