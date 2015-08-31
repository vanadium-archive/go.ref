// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memstore provides a simple, in-memory implementation of store.Store.
// Since it's a prototype implementation, it makes no attempt to be performant.
package memstore

import (
	"fmt"
	"sync"

	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/transactions"
)

type memstore struct {
	mu   sync.Mutex
	node *store.ResourceNode
	data map[string][]byte
	err  error
}

// New creates a new memstore.
func New() store.Store {
	return transactions.Wrap(&memstore{
		data: map[string][]byte{},
		node: store.NewResourceNode(),
	})
}

// Close implements the store.Store interface.
func (st *memstore) Close() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return store.ConvertError(st.err)
	}
	st.node.Close()
	st.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore)
	return nil
}

// Get implements the store.StoreReader interface.
func (st *memstore) Get(key, valbuf []byte) ([]byte, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return valbuf, store.ConvertError(st.err)
	}
	value, ok := st.data[string(key)]
	if !ok {
		return valbuf, verror.New(store.ErrUnknownKey, nil, string(key))
	}
	return store.CopyBytes(valbuf, value), nil
}

// Scan implements the store.StoreReader interface.
func (st *memstore) Scan(start, limit []byte) store.Stream {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return &store.InvalidStream{Error: st.err}
	}
	// TODO(sadovsky): Close snapshot once stream is closed or canceled.
	return newSnapshot(st, st.node).Scan(start, limit)
}

// NewSnapshot implements the store.Store interface.
func (st *memstore) NewSnapshot() store.Snapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return &store.InvalidSnapshot{Error: st.err}
	}
	return newSnapshot(st, st.node)
}

// WriteBatch implements the transactions.BatchStore interface.
func (st *memstore) WriteBatch(batch ...transactions.WriteOp) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return store.ConvertError(st.err)
	}
	for _, write := range batch {
		switch write.T {
		case transactions.PutOp:
			st.data[string(write.Key)] = write.Value
		case transactions.DeleteOp:
			delete(st.data, string(write.Key))
		default:
			panic(fmt.Sprintf("unknown write operation type: %v", write.T))
		}
	}
	return nil
}
