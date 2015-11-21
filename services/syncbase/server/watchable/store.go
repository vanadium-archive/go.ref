// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package watchable provides a Syncbase-specific store.Store wrapper that
// provides versioned storage for specified prefixes and maintains a watchable
// log of operations performed on versioned records. This log forms the basis
// for the implementation of client-facing watch as well as the sync module's
// internal watching of store updates.
//
// LogEntry records are stored chronologically, using keys of the form
// "$log:<seq>". Sequence numbers are zero-padded to ensure that the
// lexicographic order matches the numeric order.
//
// Version number records are stored using keys of the form "$version:<key>",
// where <key> is the client-specified key.
package watchable

import (
	"fmt"
	"strings"
	"sync"

	pubutil "v.io/v23/syncbase/util"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/vclock"
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
func Wrap(st store.Store, vclock *vclock.VClock, opts *Options) (Store, error) {
	seq, err := getNextLogSeq(st)
	if err != nil {
		return nil, err
	}
	return &wstore{
		ist:     st,
		watcher: newWatcher(),
		opts:    opts,
		seq:     seq,
		vclock:  vclock,
	}, nil
}

type wstore struct {
	ist     store.Store
	watcher *watcher
	opts    *Options
	mu      sync.Mutex     // held during transaction commits; protects seq
	seq     uint64         // the next sequence number to be used for a new commit
	vclock  *vclock.VClock // used to provide write timestamps
}

var _ Store = (*wstore)(nil)

// Close implements the store.Store interface.
func (st *wstore) Close() error {
	st.watcher.close()
	return st.ist.Close()
}

// Get implements the store.StoreReader interface.
func (st *wstore) Get(key, valbuf []byte) ([]byte, error) {
	if !st.managesKey(key) {
		return st.ist.Get(key, valbuf)
	}
	sn := newSnapshot(st)
	defer sn.Abort()
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
	return store.RunInTransaction(st, func(tx store.Transaction) error {
		return tx.Put(key, value)
	})
}

// Delete implements the store.StoreWriter interface.
func (st *wstore) Delete(key []byte) error {
	// Use watchable.Store transaction so this op gets logged.
	return store.RunInTransaction(st, func(tx store.Transaction) error {
		return tx.Delete(key)
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

// GetOptions returns the options configured on a watchable.Store.
// TODO(rdaoud): expose watchable store through an interface and change this
// function to be a method on the store.
func GetOptions(st store.Store) (*Options, error) {
	wst := st.(*wstore)
	return wst.opts, nil
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
