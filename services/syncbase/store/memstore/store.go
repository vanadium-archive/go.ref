// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memstore provides a simple, in-memory implementation of store.Store.
// Since it's a prototype implementation, it makes no attempt to be performant.
package memstore

import (
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

type memstore struct {
	mu   sync.Mutex
	data map[string][]byte
	// Most recent sequence number handed out.
	lastSeq uint64
	// Value of lastSeq at the time of the most recent commit.
	lastCommitSeq uint64
}

var _ store.Store = (*memstore)(nil)

// New creates a new memstore.
func New() store.Store {
	return &memstore{data: map[string][]byte{}}
}

// Close implements the store.Store interface.
func (st *memstore) Close() error {
	// TODO(sadovsky): Make all subsequent method calls and stream advances fail.
	return nil
}

// Get implements the store.StoreReader interface.
func (st *memstore) Get(key, valbuf []byte) ([]byte, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	value, ok := st.data[string(key)]
	if !ok {
		return nil, &store.ErrUnknownKey{Key: string(key)}
	}
	return store.CopyBytes(valbuf, value), nil
}

// Scan implements the store.StoreReader interface.
func (st *memstore) Scan(start, end []byte) (store.Stream, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return newStream(newSnapshot(st), start, end), nil
}

// Put implements the store.StoreWriter interface.
func (st *memstore) Put(key, value []byte) error {
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Put(key, value)
	})
}

// Delete implements the store.StoreWriter interface.
func (st *memstore) Delete(key []byte) error {
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Delete(key)
	})
}

// NewTransaction implements the store.Store interface.
func (st *memstore) NewTransaction() store.Transaction {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeq++
	return newTransaction(st, st.lastSeq)
}

// NewSnapshot implements the store.Store interface.
func (st *memstore) NewSnapshot() store.Snapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	return newSnapshot(st)
}
