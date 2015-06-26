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
	tx := &transaction{
		node:        node,
		st:          st,
		sn:          newSnapshot(st, node),
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
		return convertError(tx.err)
	}
	if tx.expired() {
		return verror.New(verror.ErrBadState, nil, store.ErrMsgExpiredTxn)
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

	// Reflect the state of the transaction: the "puts" and "deletes"
	// override the values in the transaction snapshot.
	keyStr := string(key)
	if val, ok := tx.puts[keyStr]; ok {
		return val, nil
	}
	if _, ok := tx.deletes[keyStr]; ok {
		return valbuf, verror.New(store.ErrUnknownKey, nil, keyStr)
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

	// Create an array of store.WriteOps as it is needed to merge
	// the snaphot stream with the uncommitted changes.
	var writes []store.WriteOp
	for k, v := range tx.puts {
		writes = append(writes, store.WriteOp{T: store.PutOp, Key: []byte(k), Value: v})
	}
	for k, _ := range tx.deletes {
		writes = append(writes, store.WriteOp{T: store.DeleteOp, Key: []byte(k), Value: []byte{}})
	}

	// Return a stream which merges the snaphot stream with the uncommitted changes.
	return store.MergeWritesWithStream(tx.sn, writes, start, limit)
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
	tx.err = verror.New(verror.ErrBadState, nil, store.ErrMsgCommittedTxn)
	tx.node.Close()
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	if tx.seq <= tx.st.lastCommitSeq {
		return store.NewErrConcurrentTransaction(nil)
	}
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
	tx.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgAbortedTxn)
	return nil
}
