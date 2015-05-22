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
	mu   sync.Mutex
	data map[string][]byte
	err  error
}

var _ store.Snapshot = (*snapshot)(nil)

// Assumes st lock is held.
func newSnapshot(st *memstore) *snapshot {
	dataCopy := map[string][]byte{}
	for k, v := range st.data {
		dataCopy[k] = v
	}
	return &snapshot{data: dataCopy}
}

// Close implements the store.Snapshot interface.
func (s *snapshot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return store.WrapError(s.err)
	}
	s.err = verror.New(verror.ErrCanceled, nil, "closed snapshot")
	return nil
}

// Get implements the store.StoreReader interface.
func (s *snapshot) Get(key, valbuf []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return valbuf, store.WrapError(s.err)
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
		return &store.InvalidStream{s.err}
	}
	return newStream(s, start, limit)
}
