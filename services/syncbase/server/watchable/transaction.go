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
	"v.io/v23/context"
	"v.io/v23/verror"
)

type transaction struct {
	itx store.Transaction
	st  *wstore
	mu  sync.Mutex // protects the fields below
	err error
	ops []Op
	// fromSync is true when a transaction is created by sync.  This causes
	// the log entries written at commit time to have their "FromSync" field
	// set to true.  That in turn causes the sync watcher to filter out such
	// updates since sync already knows about them (echo suppression).
	fromSync bool
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
		return valbuf, convertError(tx.err)
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
		return convertError(tx.err)
	}
	if !tx.st.managesKey(key) {
		return tx.itx.Put(key, value)
	}

	version, err := putVersioned(tx.itx, key, value)
	if err != nil {
		return err
	}
	tx.ops = append(tx.ops, &OpPut{PutOp{Key: key, Version: version}})
	return nil
}

// Delete implements the store.StoreWriter interface.
func (tx *transaction) Delete(key []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return convertError(tx.err)
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
		return convertError(tx.err)
	}
	tx.err = verror.New(verror.ErrBadState, nil, store.ErrMsgCommittedTxn)
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	// Check if there is enough space left in the sequence number.
	if (math.MaxUint64 - tx.st.seq) < uint64(len(tx.ops)) {
		return verror.New(verror.ErrInternal, nil, "seq maxed out")
	}
	// Write LogEntry records.
	timestamp := tx.st.clock.Now().UnixNano()
	seq := tx.st.seq
	for i, op := range tx.ops {
		key := getLogEntryKey(seq)
		value := &LogEntry{
			Op:              op,
			CommitTimestamp: timestamp,
			FromSync:        tx.fromSync,
			Continued:       i < len(tx.ops)-1,
		}
		if err := util.PutObject(tx.itx, key, value); err != nil {
			return err
		}
		seq++
	}
	if err := tx.itx.Commit(); err != nil {
		return err
	}
	tx.st.seq = seq
	return nil
}

// Abort implements the store.Transaction interface.
func (tx *transaction) Abort() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return convertError(tx.err)
	}
	tx.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgAbortedTxn)
	return tx.itx.Abort()
}

// AddSyncGroupOp injects a SyncGroup operation notification in the log entries
// that the transaction writes when it is committed.  It allows the SyncGroup
// operations (create, join, leave, destroy) to notify the sync watcher of the
// change at its proper position in the timeline (the transaction commit).
// Note: this is an internal function used by sync, not part of the interface.
func AddSyncGroupOp(ctx *context.T, tx store.Transaction, prefixes []string, remove bool) error {
	wtx := tx.(*transaction)
	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}
	wtx.ops = append(wtx.ops, &OpSyncGroup{SyncGroupOp{Prefixes: prefixes, Remove: remove}})
	return nil
}

// AddSyncSnapshotOp injects a sync snapshot operation notification in the log
// entries that the transaction writes when it is committed.  It allows the
// SyncGroup create or join operations to notify the sync watcher of the
// current keys and their versions to use when initializing the sync metadata
// at the point in the timeline when these keys become syncable (at commit).
// Note: this is an internal function used by sync, not part of the interface.
func AddSyncSnapshotOp(ctx *context.T, tx store.Transaction, key, version []byte) error {
	wtx := tx.(*transaction)
	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}
	if !wtx.st.managesKey(key) {
		return verror.New(verror.ErrInternal, ctx,
			fmt.Sprintf("cannot create SyncSnapshotOp on unmanaged key: %s", string(key)))
	}
	wtx.ops = append(wtx.ops, &OpSyncSnapshot{SyncSnapshotOp{Key: key, Version: version}})
	return nil
}

// SetTransactionFromSync marks this transaction as created by sync as opposed
// to one created by an application.  The net effect is that, at commit time,
// the log entries written are marked as made by sync.  This allows the sync
// Watcher to ignore them (echo suppression) because it made these updates.
// Note: this is an internal function used by sync, not part of the interface.
// TODO(rdaoud): support a generic echo-suppression mechanism for apps as well
// maybe by having a creator ID in the transaction and log entries.
// TODO(rdaoud): fold this flag (or creator ID) into Tx options when available.
func SetTransactionFromSync(tx store.Transaction) {
	wtx := tx.(*transaction)
	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	wtx.fromSync = true
}

// Exported as a helper function for testing purposes
func getLogEntryKey(seq uint64) string {
	// Note: MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	return join(util.LogPrefix, fmt.Sprintf("%016x", seq))
}
