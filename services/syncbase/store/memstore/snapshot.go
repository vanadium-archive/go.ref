// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

type snapshot struct {
	store.SnapshotSpecImpl
	mu   sync.Mutex
	node *store.ResourceNode
	data map[string][]byte
	err  error
}

var _ store.Snapshot = (*snapshot)(nil)

// Assumes st lock is held.
func newSnapshot(st *memstore, parent *store.ResourceNode) *snapshot {
	dataCopy := make(map[string][]byte, len(st.data))
	for k, v := range st.data {
		dataCopy[k] = v
	}
	s := &snapshot{
		node: store.NewResourceNode(),
		data: dataCopy,
	}
	parent.AddChild(s.node, func() {
		s.Abort()
	})
	return s
}

// Abort implements the store.Snapshot interface.
func (s *snapshot) Abort() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return convertError(s.err)
	}
	s.node.Close()
	s.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgAbortedSnapshot)
	return nil
}

// Get implements the store.StoreReader interface.
func (s *snapshot) Get(key, valbuf []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return valbuf, convertError(s.err)
	}
	value, ok := s.data[string(key)]
	if !ok {
		return valbuf, verror.New(store.ErrUnknownKey, nil, string(key))
	}
	return store.CopyBytes(valbuf, value), nil
}

// Scan implements the store.StoreReader interface.
func (s *snapshot) Scan(start, limit []byte) store.Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return &store.InvalidStream{Error: s.err}
	}
	return newStream(s, s.node, start, limit)
}
