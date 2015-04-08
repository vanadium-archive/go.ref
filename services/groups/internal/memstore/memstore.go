// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memstore provides a simple, in-memory implementation of server.Store.
// Since it's a prototype implementation, it doesn't bother with entry-level
// locking.
package memstore

import (
	"strconv"
	"sync"

	"v.io/x/ref/services/groups/internal/server"
)

type entry struct {
	v       interface{}
	version int
}

type memstore struct {
	mu   sync.Mutex
	data map[string]*entry
}

var _ server.Store = (*memstore)(nil)

func New() server.Store {
	return &memstore{data: map[string]*entry{}}
}

func (st *memstore) Get(k string) (v interface{}, version string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.data[k]
	if !ok {
		return nil, "", &server.ErrUnknownKey{Key: k}
	}
	return e.v, strconv.Itoa(e.version), nil
}

func (st *memstore) Insert(k string, v interface{}) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.data[k]; ok {
		return &server.ErrKeyAlreadyExists{Key: k}
	}
	st.data[k] = &entry{v: v}
	return nil
}

func (st *memstore) Update(k string, v interface{}, version string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.data[k]
	if !ok {
		return &server.ErrUnknownKey{Key: k}
	}
	if err := e.checkVersion(version); err != nil {
		return err
	}
	e.v = v
	e.version++
	return nil
}

func (st *memstore) Delete(k string, version string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.data[k]
	if !ok {
		return &server.ErrUnknownKey{Key: k}
	}
	if err := e.checkVersion(version); err != nil {
		return err
	}
	delete(st.data, k)
	return nil
}

func (e *entry) checkVersion(version string) error {
	newVersion := strconv.Itoa(e.version)
	if version != newVersion {
		return &server.ErrBadVersion{}
	}
	return nil
}
