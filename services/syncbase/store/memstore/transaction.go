// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"sync"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

const (
	txnTimeout = time.Duration(5) * time.Second
)

type transaction struct {
	mu   sync.Mutex
	node *store.ResourceNode
	st   *memstore
	sn   *snapshot
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

func newTransaction(st *memstore, parent *store.ResourceNode, seq uint64) *transaction {
	node := store.NewResourceNode()
	sn := newSnapshot(st, node)
	tx := &transaction{
		node:        node,
		st:          st,
		sn:          sn,
		seq:         seq,
		createdTime: time.Now(),
		puts:        map[string][]byte{},
		deletes:     map[string]struct{}{},
	}
	parent.AddChild(tx.node, func() {
		tx.Abort()
	})
	return tx
}

func (tx *transaction) expired() bool {
	return time.Now().After(tx.createdTime.Add(txnTimeout))
}

func (tx *transaction) error() error {
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	if tx.expired() {
		return verror.New(verror.ErrBadState, nil, "expired transaction")
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
	return tx.sn.Scan(start, limit)
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
	tx.node.Close()
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock() // note, defer is last-in-first-out
	if tx.seq <= tx.st.lastCommitSeq {
		// Once Commit() has failed with store.ErrConcurrentTransaction, subsequent
		// ops on the transaction will fail with the following error.
		tx.err = verror.New(verror.ErrBadState, nil, "already attempted to commit transaction")
		return store.NewErrConcurrentTransaction(nil)
	}
	tx.err = verror.New(verror.ErrBadState, nil, "committed transaction")
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
	tx.node.Close()
	tx.err = verror.New(verror.ErrCanceled, nil, "aborted transaction")
	return nil
}
