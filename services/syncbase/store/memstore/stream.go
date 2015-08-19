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
	node      *store.ResourceNode
	sn        *snapshot
	keys      []string
	currIndex int
	currKey   *string
	err       error
	done      bool
}

var _ store.Stream = (*stream)(nil)

func newStream(sn *snapshot, parent *store.ResourceNode, start, limit []byte) *stream {
	keys := []string{}
	for k := range sn.data {
		if k >= string(start) && (len(limit) == 0 || k < string(limit)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	s := &stream{
		node:      store.NewResourceNode(),
		sn:        sn,
		keys:      keys,
		currIndex: -1,
	}
	parent.AddChild(s.node, func() {
		s.Cancel()
	})
	return s
}

// Advance implements the store.Stream interface.
func (s *stream) Advance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currKey = nil
	if s.done {
		return false
	}
	s.currIndex++
	if s.currIndex < len(s.keys) {
		s.currKey = &s.keys[s.currIndex]
	} else {
		s.done = true
		s.currKey = nil
	}
	return !s.done
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
	return convertError(s.err)
}

// Cancel implements the store.Stream interface.
func (s *stream) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	s.node.Close()
	s.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgCanceledStream)
}
