// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"errors"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

var (
	errClosedSnapshot = errors.New("closed snapshot")
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

func (s *snapshot) error() error {
	return s.err
}

// Close implements the store.Snapshot interface.
func (s *snapshot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.error(); err != nil {
		return err
	}
	s.err = errClosedSnapshot
	return nil
}

// Get implements the store.StoreReader interface.
func (s *snapshot) Get(key, valbuf []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.error(); err != nil {
		return valbuf, err
	}
	value, ok := s.data[string(key)]
	if !ok {
		return valbuf, &store.ErrUnknownKey{Key: string(key)}
	}
	return store.CopyBytes(valbuf, value), nil
}

// Scan implements the store.StoreReader interface.
func (s *snapshot) Scan(start, end []byte) store.Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.error(); err != nil {
		return &store.InvalidStream{err}
	}
	return newStream(s, start, end)
}
