// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package watchable provides a Syncbase-specific store.Store wrapper that
// provides versioned storage for specified prefixes and maintains a watchable
// log of operations performed on versioned records. This log forms the basis
// for the implementation of client-facing watch as well as the sync module's
// internal watching of store updates.
//
// Log entries are keyed in reverse chronological order. More specifically, the
// LogEntry key format is "$log:<seq>:<txSeq>", where <seq> is (MaxUint64-seq)
// and <txSeq> is (MaxUint16-txSeq). All numbers are zero-padded to ensure that
// the lexicographic order matches the numeric order. Thus, clients implementing
// ResumeMarkers (i.e. implementing the watch API) should use
// fmt.Sprintf("%016x", MaxUint64-marker) to convert external markers to
// internal LogEntry key prefixes.
//
// Version number records are stored with keys of the form "$version:<key>",
// where <key> is the client-specified key.
package watchable

// TODO(sadovsky): Write unit tests. (As of May 2015 we're still iterating on
// the design for how to expose a "watch" API from the storage engine, and we
// don't want to write lots of tests prematurely.)
// TODO(sadovsky): Expose helper functions for constructing LogEntry keys.
// TODO(sadovsky): Allow clients to subscribe via Go channel.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	pubutil "v.io/syncbase/v23/syncbase/util"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

const (
	MaxUint16 uint64 = 1<<16 - 1 // 0xffff
	MaxUint64 uint64 = 1<<64 - 1 // 0xffffffffffffffff
)

// Store is a store.Store that provides versioned storage and a watchable oplog.
// TODO(sadovsky): Extend interface.
type Store interface {
	store.Store
}

// Options configures a watchable.Store.
type Options struct {
	// Key prefixes to version and log. If nil, all keys are managed.
	ManagedPrefixes []string
}

// Wrap returns a watchable.Store that wraps the given store.Store.
func Wrap(st store.Store, opts *Options) (Store, error) {
	// Determine initial value for seq.
	var seq uint64 = 0
	it := st.Scan([]byte(util.LogPrefix), []byte(""))
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
	return &wstore{ist: st, opts: opts, seq: seq}, nil
}

type wstore struct {
	ist  store.Store
	opts *Options
	mu   sync.Mutex // held during transaction commits; protects seq
	seq  uint64     // sequence number, for commits
}

var _ Store = (*wstore)(nil)

// TODO(sadovsky): Decide whether to copy []byte's vs. requiring clients not to
// modify passed-in []byte's. (In fact, this should be spelled out in the
// store.Store API contract.)

// Close implements the store.Store interface.
func (st *wstore) Close() error {
	return st.ist.Close()
}

// Get implements the store.StoreReader interface.
func (st *wstore) Get(key, valbuf []byte) ([]byte, error) {
	if !st.managesKey(key) {
		return st.ist.Get(key, valbuf)
	}
	sn := newSnapshot(st)
	defer sn.Close()
	return sn.Get(key, valbuf)
}

// Scan implements the store.StoreReader interface.
func (st *wstore) Scan(start, limit []byte) store.Stream {
	if !st.managesRange(start, limit) {
		return st.ist.Scan(start, limit)
	}
	// TODO(sadovsky): Close snapshot once stream is finished or canceled.
	return newSnapshot(st).Scan(start, limit)
}

// Put implements the store.StoreWriter interface.
func (st *wstore) Put(key, value []byte) error {
	// Use watchable.Store transaction so this op gets logged.
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Put(key, value)
	})
}

// Delete implements the store.StoreWriter interface.
func (st *wstore) Delete(key []byte) error {
	// Use watchable.Store transaction so this op gets logged.
	return store.RunInTransaction(st, func(st store.StoreReadWriter) error {
		return st.Delete(key)
	})
}

// NewTransaction implements the store.Store interface.
func (st *wstore) NewTransaction() store.Transaction {
	return newTransaction(st)
}

// NewSnapshot implements the store.Store interface.
func (st *wstore) NewSnapshot() store.Snapshot {
	return newSnapshot(st)
}

////////////////////////////////////////
// Internal helpers

func (st *wstore) managesKey(key []byte) bool {
	if st.opts.ManagedPrefixes == nil {
		return true
	}
	ikey := string(key)
	// TODO(sadovsky): Optimize, e.g. use binary search (here and below).
	for _, p := range st.opts.ManagedPrefixes {
		if strings.HasPrefix(ikey, p) {
			return true
		}
	}
	return false
}

func (st *wstore) managesRange(start, limit []byte) bool {
	if st.opts.ManagedPrefixes == nil {
		return true
	}
	istart, ilimit := string(start), string(limit)
	for _, p := range st.opts.ManagedPrefixes {
		pstart, plimit := pubutil.PrefixRangeStart(p), pubutil.PrefixRangeLimit(p)
		if pstart <= istart && ilimit <= plimit {
			return true
		}
		if !(plimit <= istart || ilimit <= pstart) {
			// If this happens, there's a bug in the Syncbase server implementation.
			panic(fmt.Sprintf("partial overlap: %q %q %q", p, start, limit))
		}
	}
	return false
}
