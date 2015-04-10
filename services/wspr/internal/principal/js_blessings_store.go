// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package principal

import (
	"fmt"
	"reflect"
	"sync"

	"v.io/v23/security"
)

type refToBlessings struct {
	blessings security.Blessings
	refCount  int
}

// JSBlessingsHandles is a store for Blessings in use by JS code.
//
// We don't pass the full Blessings object to avoid serializing
// and deserializing a potentially huge forest of blessings.
// Instead we pass to JS a handle to a Blessings object and have
// all operations involving cryptographic operations call into go.
type JSBlessingsHandles struct {
	mu         sync.Mutex
	lastHandle BlessingsHandle
	store      map[BlessingsHandle]*refToBlessings
}

// NewJSBlessingsHandles returns a newly initialized JSBlessingsHandles
func NewJSBlessingsHandles() *JSBlessingsHandles {
	return &JSBlessingsHandles{
		store: map[BlessingsHandle]*refToBlessings{},
	}
}

// GetOrAddHandle looks for a corresponding blessing handle and adds one if not found.
func (s *JSBlessingsHandles) GetOrAddHandle(blessings security.Blessings) BlessingsHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Look for an existing blessing.
	for handle, ref := range s.store {
		if reflect.DeepEqual(blessings, ref.blessings) {
			ref.refCount++
			return handle
		}
	}

	// Otherwise add it
	s.lastHandle++
	handle := s.lastHandle
	s.store[handle] = &refToBlessings{
		blessings: blessings,
		refCount:  1,
	}
	return handle
}

// RemoveReference indicates the removal of a reference to
// the Blessings associated with the handle.
func (s *JSBlessingsHandles) RemoveReference(handle BlessingsHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.store[handle]
	if !ok {
		return fmt.Errorf("Could not find reference to handle being removed: %v", handle)
	}
	ref.refCount--
	if ref.refCount == 0 {
		delete(s.store, handle)
	}
	if ref.refCount < 0 {
		return fmt.Errorf("Unexpected negative ref count")
	}
	return nil
}

// GetBlessings returns the Blessings represented by the handle. Returns nil
// if no Blessings exists for the handle.
func (s *JSBlessingsHandles) GetBlessings(handle BlessingsHandle) security.Blessings {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.store[handle]
	if !ok {
		return security.Blessings{}
	}
	return ref.blessings
}
