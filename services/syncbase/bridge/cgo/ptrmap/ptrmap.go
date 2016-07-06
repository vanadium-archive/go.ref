// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ptrmap

import (
	"fmt"
	"sync"
)

func New() *ptrMap {
	return &ptrMap{
		refs: make(map[uintptr]interface{}),
	}
}

type ptrMap struct {
	refs map[uintptr]interface{}
	lock sync.Mutex
}

func (r *ptrMap) Set(ptr uintptr, val interface{}) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	if _, ok := r.refs[ptr]; ok {
		return fmt.Errorf("already have existing value at address %v", ptr)
	}
	r.refs[ptr] = val
	return nil
}

func (r *ptrMap) Get(ptr uintptr) interface{} {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.refs[ptr]
}

func (r *ptrMap) Remove(ptr uintptr) interface{} {
	r.lock.Lock()
	defer r.lock.Unlock()
	val, ok := r.refs[ptr]
	if ok {
		delete(r.refs, ptr)
	}
	return val
}
