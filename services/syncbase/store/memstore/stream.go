// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"sort"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

type stream struct {
	mu        sync.Mutex
	sn        *snapshot
	keys      []string
	currIndex int
	currKey   *string
	err       error
}

var _ store.Stream = (*stream)(nil)

func newStream(sn *snapshot, start, end []byte) *stream {
	keys := []string{}
	for k := range sn.data {
		if k >= string(start) && k < string(end) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return &stream{
		sn:        sn,
		keys:      keys,
		currIndex: -1,
	}
}

// Advance implements the store.Stream interface.
func (s *stream) Advance() bool {
	// TODO(sadovsky): Advance should return false and Err should return a non-nil
	// error if the Store was closed, or if the Snapshot was closed, or if the
	// Transaction was committed or aborted (or timed out).
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		s.currKey = nil
	} else {
		s.currIndex++
		if s.currIndex < len(s.keys) {
			s.currKey = &s.keys[s.currIndex]
		} else {
			s.currKey = nil
		}
	}
	return s.currKey != nil
}

// Key implements the store.Stream interface.
func (s *stream) Key(keybuf []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currKey == nil {
		panic("nothing staged")
	}
	return store.CopyBytes(keybuf, []byte(*s.currKey))
}

// Value implements the store.Stream interface.
func (s *stream) Value(valbuf []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currKey == nil {
		panic("nothing staged")
	}
	return store.CopyBytes(valbuf, s.sn.data[*s.currKey])
}

// Err implements the store.Stream interface.
func (s *stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Cancel implements the store.Stream interface.
func (s *stream) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = verror.New(verror.ErrCanceled, nil)
}
