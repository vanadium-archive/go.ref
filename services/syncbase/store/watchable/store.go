// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package watchable provides a store.Store that maintains a commit log. In
// Syncbase, this log forms the basis for the implementation of client-facing
// watch as well as the sync module's watching of store commits.
//
// Log entries are keyed in reverse chronological order. More specifically, the
// LogEntry key format is "$log:<seq>:<txSeq>", where <seq> is (MaxUint64-seq)
// and <txSeq> is (MaxUint16-txSeq). All numbers are zero-padded to ensure that
// the lexicographic order matches the numeric order. Thus, clients implementing
// ResumeMarkers (i.e. implementing the watch API) should use
// fmt.Sprintf("%020d", MaxUint64-marker) to convert external markers to
// internal LogEntry key prefixes.
package watchable

// TODO(sadovsky): Write unit tests. (As of 2015-05-26 we're still iterating on
// the design for how to expose a "watch" API from the storage engine, and we
// don't want to write lots of tests prematurely.)
// TODO(sadovsky): Expose helper functions for constructing LogEntry keys.
// TODO(sadovsky): Allow clients to subscribe via Go channel.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
	"v.io/v23/vom"
)

const (
	LogPrefix        = "$log"
	MaxUint16 uint64 = 1<<16 - 1 // 5 digits
	MaxUint64 uint64 = 1<<64 - 1 // 20 digits
)

// Store is a store.Store that maintains a commit log.
type Store store.Store

// Wrap returns a watchable.Store that wraps the given store.Store.
func Wrap(st store.Store) (Store, error) {
	it := st.Scan([]byte(LogPrefix), []byte(""))
	var seq uint64 = 0
	for it.Advance() {
		key := string(it.Key(nil))
		parts := split(key)
		if len(parts) != 3 {
			panic("wrong number of parts: " + key)
		}
		invSeq, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			panic("failed to parse invSeq: " + key)
		}
		seq = MaxUint64 - invSeq
		it.Cancel()
	}
	if err := it.Err(); err != nil && verror.ErrorID(err) != verror.ErrCanceled.ID {
		return nil, err
	}
	return &wstore{Store: st, seq: seq}, nil
}

type wstore struct {
	store.Store
	mu  sync.Mutex // held during transaction commits; protects seq
	seq uint64     // sequence number, for commits
}

type transaction struct {
	store.Transaction
	st  *wstore
	mu  sync.Mutex // protects the fields below
	ops []Op
}

var (
	_ Store             = (*wstore)(nil)
	_ store.Transaction = (*transaction)(nil)
)

// TODO(sadovsky): Decide whether to copy []bytes vs. requiring clients not to
// modify passed-in []bytes.

func (st *wstore) Put(key, value []byte) error {
	// Use watchable.Store transaction so this op gets logged.
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Put(key, value)
	})
}

func (st *wstore) Delete(key []byte) error {
	// Use watchable.Store transaction so this op gets logged.
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Delete(key)
	})
}

func (st *wstore) NewTransaction() store.Transaction {
	return &transaction{Transaction: st.Store.NewTransaction(), st: st}
}

func (tx *transaction) Get(key, valbuf []byte) ([]byte, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	valbuf, err := tx.Transaction.Get(key, valbuf)
	if err == nil || verror.ErrorID(err) == store.ErrUnknownKey.ID {
		tx.ops = append(tx.ops, &OpGet{GetOp{Key: key}})
	}
	return valbuf, err
}

func (tx *transaction) Scan(start, limit []byte) store.Stream {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	it := tx.Transaction.Scan(start, limit)
	if it.Err() == nil {
		tx.ops = append(tx.ops, &OpScan{ScanOp{Start: start, Limit: limit}})
	}
	return it
}

func (tx *transaction) Put(key, value []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	err := tx.Transaction.Put(key, value)
	if err == nil {
		tx.ops = append(tx.ops, &OpPut{PutOp{Key: key, Value: value}})
	}
	return err
}

func (tx *transaction) Delete(key []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	err := tx.Transaction.Delete(key)
	if err == nil {
		tx.ops = append(tx.ops, &OpDelete{DeleteOp{Key: key}})
	}
	return err
}

func (tx *transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.st.mu.Lock()
	defer tx.st.mu.Unlock()
	// Check sequence numbers.
	if uint64(len(tx.ops)) > MaxUint16 {
		return verror.New(verror.ErrInternal, nil, "too many ops")
	}
	if tx.st.seq == MaxUint64 {
		return verror.New(verror.ErrInternal, nil, "seq maxed out")
	}
	// Write LogEntry records.
	// TODO(sadovsky): Use a more efficient lexicographic number encoding.
	keyPrefix := join(LogPrefix, fmt.Sprintf("%020d", MaxUint64-tx.st.seq))
	for txSeq, op := range tx.ops {
		key := join(keyPrefix, fmt.Sprintf("%05d", MaxUint16-uint64(txSeq)))
		value := &LogEntry{
			Op: op,
			// TODO(sadovsky): This information is also captured in LogEntry keys.
			// Optimize to avoid redundancy.
			Continued: txSeq < len(tx.ops)-1,
		}
		if err := put(tx.Transaction, key, value); err != nil {
			return err
		}
	}
	if err := tx.Transaction.Commit(); err != nil {
		return err
	}
	tx.st.seq++
	return nil
}

////////////////////////////////////////
// Internal helpers

func join(parts ...string) string {
	return strings.Join(parts, ":")
}

func split(key string) []string {
	return strings.Split(key, ":")
}

func put(st store.StoreWriter, k string, v interface{}) error {
	bytes, err := vom.Encode(v)
	if err != nil {
		return err
	}
	return st.Put([]byte(k), bytes)
}
