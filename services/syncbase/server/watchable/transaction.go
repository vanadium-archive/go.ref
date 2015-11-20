// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"math"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
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

func cp(src []byte) []byte {
	dst := make([]byte, len(src))
	for i := 0; i < len(src); i++ {
		dst[i] = src[i]
	}
	return dst
}

func cpStrings(src []string) []string {
	dst := make([]string, len(src))
	for i := 0; i < len(src); i++ {
		dst[i] = src[i]
	}
	return dst
}

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
		tx.ops = append(tx.ops, &OpGet{GetOp{Key: cp(key)}})
	}
	return valbuf, err
}

// Scan implements the store.StoreReader interface.
func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return &store.InvalidStream{Error: tx.err}
	}
	var it store.Stream
	if !tx.st.managesRange(start, limit) {
		it = tx.itx.Scan(start, limit)
	} else {
		it = newStreamVersioned(tx.itx, start, limit)
		tx.ops = append(tx.ops, &OpScan{ScanOp{Start: cp(start), Limit: cp(limit)}})
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
	tx.ops = append(tx.ops, &OpPut{PutOp{Key: cp(key), Version: version}})
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
		return tx.itx.Delete(key)
	}
	err = deleteVersioned(tx.itx, key)
	if err != nil {
		return err
	}
	tx.ops = append(tx.ops, &OpDelete{DeleteOp{Key: cp(key)}})
	return nil
}

// Commit implements the store.Transaction interface.
func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.err != nil {
		return convertError(tx.err)
	}
	// Note: ErrMsgCommittedTxn is used to prevent clients from calling Commit
	// more than once on a given transaction.
	tx.err = verror.New(verror.ErrBadState, nil, store.ErrMsgCommittedTxn)
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	// NOTE(sadovsky): It's not clear whether we should call Now() under the
	// tx.st.mu lock (there are pros and cons). However, we plan to add a caching
	// layer to VClock, at which point Now() will be near-instant.
	now, err := tx.st.vclock.Now()
	if err != nil {
		return convertError(err)
	}
	// Check if there is enough space left in the sequence number.
	if (math.MaxUint64 - tx.st.seq) < uint64(len(tx.ops)) {
		return verror.New(verror.ErrInternal, nil, "seq maxed out")
	}
	// Write LogEntry records.
	seq := tx.st.seq
	for i, op := range tx.ops {
		key := logEntryKey(seq)
		value := &LogEntry{
			Op:              op,
			CommitTimestamp: now.UnixNano(),
			FromSync:        tx.fromSync,
			Continued:       i < len(tx.ops)-1,
		}
		if err := util.Put(nil, tx.itx, key, value); err != nil {
			return err
		}
		seq++
	}
	if err := tx.itx.Commit(); err != nil {
		return err
	}
	tx.st.seq = seq
	tx.st.watcher.broadcastUpdates()
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

// GetStoreTime returns the current time from the given transaction store.
func GetStoreTime(ctx *context.T, tx store.Transaction) (time.Time, error) {
	wtx := tx.(*transaction)
	return wtx.st.vclock.Now()
}

// AddSyncgroupOp injects a syncgroup operation notification in the log entries
// that the transaction writes when it is committed.  It allows the syncgroup
// operations (create, join, leave, destroy) to notify the sync watcher of the
// change at its proper position in the timeline (the transaction commit).
// Note: this is an internal function used by sync, not part of the interface.
func AddSyncgroupOp(ctx *context.T, tx store.Transaction, gid interfaces.GroupId, prefixes []string, remove bool) error {
	wtx := tx.(*transaction)
	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}
	// Make a defensive copy of prefixes slice.
	wtx.ops = append(wtx.ops, &OpSyncgroup{SyncgroupOp{SgId: gid, Prefixes: cpStrings(prefixes), Remove: remove}})
	return nil
}

// AddSyncSnapshotOp injects a sync snapshot operation notification in the log
// entries that the transaction writes when it is committed.  It allows the
// syncgroup create or join operations to notify the sync watcher of the
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
		return verror.New(verror.ErrInternal, ctx, fmt.Sprintf("cannot create SyncSnapshotOp on unmanaged key: %s", string(key)))
	}
	wtx.ops = append(wtx.ops, &OpSyncSnapshot{SyncSnapshotOp{Key: cp(key), Version: cp(version)}})
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

// GetVersion returns the current version of a managed key. This method is used
// by the Sync module when the initiator is attempting to add new versions of
// objects. Reading the version key is used for optimistic concurrency
// control. At minimum, an object implementing the StoreReader interface is
// required since this is a Get operation.
func GetVersion(ctx *context.T, st store.StoreReader, key []byte) ([]byte, error) {
	switch w := st.(type) {
	case *snapshot:
		return getVersion(w.isn, key)
	case *transaction:
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.err != nil {
			return nil, convertError(w.err)
		}
		return getVersion(w.itx, key)
	case *wstore:
		return getVersion(w.ist, key)
	}
	return nil, verror.New(verror.ErrInternal, ctx, "unsupported store type")
}

// GetAtVersion returns the value of a managed key at the requested
// version. This method is used by the Sync module when the responder needs to
// send objects over the wire. At minimum, an object implementing the
// StoreReader interface is required since this is a Get operation.
func GetAtVersion(ctx *context.T, st store.StoreReader, key, valbuf, version []byte) ([]byte, error) {
	switch w := st.(type) {
	case *snapshot:
		return getAtVersion(w.isn, key, valbuf, version)
	case *transaction:
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.err != nil {
			return valbuf, convertError(w.err)
		}
		return getAtVersion(w.itx, key, valbuf, version)
	case *wstore:
		return getAtVersion(w.ist, key, valbuf, version)
	}
	return nil, verror.New(verror.ErrInternal, ctx, "unsupported store type")
}

// PutAtVersion puts a value for the managed key at the requested version. This
// method is used by the Sync module exclusively when the initiator adds objects
// with versions created on other Syncbases. At minimum, an object implementing
// the Transaction interface is required since this is a Put operation.
func PutAtVersion(ctx *context.T, tx store.Transaction, key, valbuf, version []byte) error {
	wtx := tx.(*transaction)

	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}

	// Note that we do not enqueue a PutOp in the log since this Put is not
	// updating the current version of a key.
	return wtx.itx.Put(makeAtVersionKey(key, version), valbuf)
}

// PutVersion updates the version of a managed key to the requested
// version. This method is used by the Sync module exclusively when the
// initiator selects which of the already stored versions (via PutAtVersion
// calls) becomes the current version. At minimum, an object implementing
// the Transaction interface is required since this is a Put operation.
func PutVersion(ctx *context.T, tx store.Transaction, key, version []byte) error {
	wtx := tx.(*transaction)

	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}

	if err := wtx.itx.Put(makeVersionKey(key), version); err != nil {
		return err
	}
	wtx.ops = append(wtx.ops, &OpPut{PutOp{Key: cp(key), Version: cp(version)}})
	return nil
}

// PutWithPerms puts a value for the managed key, recording the key and version
// of the prefix permissions object that granted access to this put operation.
func PutWithPerms(tx store.Transaction, key, value []byte, permsKey string) error {
	wtx := tx.(*transaction)
	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}
	if !wtx.st.managesKey(key) {
		panic(fmt.Sprintf("cannot do PutWithPerms on unmanaged key: %s", string(key)))
	}
	permsKeyBytes := []byte(permsKey)
	// NOTE: We must get the version before modifying the data because
	// the key and the permsKey might be the same. This might happen when
	// we are putting a perms object.
	permsVersion, err := getVersion(wtx.itx, permsKeyBytes)
	if err != nil {
		return err
	}
	version, err := putVersioned(wtx.itx, key, value)
	if err != nil {
		return err
	}
	wtx.ops = append(wtx.ops, &OpPut{PutOp{
		Key:         cp(key),
		Version:     version,
		PermKey:     permsKeyBytes,
		PermVersion: permsVersion,
	}})
	return nil
}

// PutVomWithPerms puts a VOM-encoded value for the managed key, recording
// the key and the version of the prefix permissions object that granted access
// to this put operation.
func PutVomWithPerms(ctx *context.T, tx store.Transaction, k string, v interface{}, permsKey string) error {
	bytes, err := vom.Encode(v)
	if err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	if err = PutWithPerms(tx, []byte(k), bytes, permsKey); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// DeleteWithPerms deletes a value for the managed key, recording the key and version
// of the prefix permissions object that granted access to this delete operation.
func DeleteWithPerms(tx store.Transaction, key []byte, permsKey string) error {
	wtx := tx.(*transaction)
	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}
	if !wtx.st.managesKey(key) {
		panic(fmt.Sprintf("cannot do DeleteWithPerms on unmanaged key: %s", string(key)))
	}
	permsKeyBytes := []byte(permsKey)
	// NOTE: We must get the version before modifying the data because
	// the key and the permsKey might be the same. This might happen when
	// we are deleting a perms object.
	permsVersion, err := getVersion(wtx.itx, permsKeyBytes)
	if err != nil {
		return err
	}
	if err := deleteVersioned(wtx.itx, key); err != nil {
		return err
	}
	wtx.ops = append(wtx.ops, &OpDelete{DeleteOp{
		Key:         cp(key),
		PermKey:     permsKeyBytes,
		PermVersion: permsVersion,
	}})
	return nil
}
