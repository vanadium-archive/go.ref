// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"math"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

type transaction struct {
	itx store.Transaction
	st  *wstore
	mu  sync.Mutex // protects the fields below
	err error
	ops []Op
}

var _ store.Transaction = (*transaction)(nil)

func newTransaction(st *wstore) *transaction {
	return &transaction{
		itx: st.ist.NewTransaction(),
		st:  st,
	}
}

// Get implements the store.StoreReader interface.
func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return valbuf, store.WrapError(tx.err)
	}
	var err error
	if !tx.st.managesKey(key) {
		valbuf, err = tx.itx.Get(key, valbuf)
	} else {
		valbuf, err = getVersioned(tx.itx, key, valbuf)
		tx.ops = append(tx.ops, &OpGet{GetOp{Key: key}})
	}
	return valbuf, err
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return &store.InvalidStream{tx.err}
	}
	var it store.Stream
	if !tx.st.managesRange(start, limit) {
		it = tx.itx.Scan(start, limit)
	} else {
		it = newStreamVersioned(tx.itx, start, limit)
		tx.ops = append(tx.ops, &OpScan{ScanOp{Start: start, Limit: limit}})
	}
	return it
}

// Put implements the store.StoreWriter interface.
func (tx *transaction) Put(key, value []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	var err error
	if !tx.st.managesKey(key) {
		err = tx.itx.Put(key, value)
	} else {
		err = putVersioned(tx.itx, key, value)
		tx.ops = append(tx.ops, &OpPut{PutOp{Key: key, Value: value}})
	}
	return err
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	var err error
	if !tx.st.managesKey(key) {
		err = tx.itx.Delete(key)
	} else {
		err = deleteVersioned(tx.itx, key)
		tx.ops = append(tx.ops, &OpDelete{DeleteOp{Key: key}})
	}
	return err
}

// Commit implements the store.Transaction interface.
func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	tx.err = verror.New(verror.ErrBadState, nil, store.ErrMsgCommittedTxn)
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	// Check sequence numbers.
	if uint64(len(tx.ops)) > math.MaxUint16 {
		return verror.New(verror.ErrInternal, nil, "too many ops")
	}
	if tx.st.seq == math.MaxUint64 {
		return verror.New(verror.ErrInternal, nil, "seq maxed out")
	}
	// Write LogEntry records.
	// Note, MaxUint16 is 0xffff and MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	keyPrefix := join(util.LogPrefix, fmt.Sprintf("%016x", tx.st.seq))
	for txSeq, op := range tx.ops {
		key := join(keyPrefix, fmt.Sprintf("%04x", txSeq))
		value := &LogEntry{
			Op: op,
			// TODO(sadovsky): This information is also captured in LogEntry keys.
			// Optimize to avoid redundancy.
			Continued: txSeq < len(tx.ops)-1,
		}
		if err := util.PutObject(tx.itx, key, value); err != nil {
			return err
		}
	}
	if err := tx.itx.Commit(); err != nil {
		return err
	}
	tx.st.seq++
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return store.WrapError(tx.err)
	}
	tx.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgAbortedTxn)
	return tx.itx.Abort()
}
