// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package iobuf

import "v.io/x/lib/vlog"

// Allocator is an allocator for Slices that tries to allocate
// contiguously.  That is, sequential allocations will tend to be contiguous,
// which means that Coalesce() will usually be able to perform coalescing
// (without copying the data).
//
//    calloc := iobuf.Allocator(...)
//    slice1 := calloc.Alloc(10)
//    slice2 := calloc.Alloc(20)
//    slices := iobuf.Coalesce([]*iobuf.Slice{slice1, slice2})
//    // slices should contain 1 element with length 30.
type Allocator struct {
	pool    *Pool
	index   uint
	reserve uint
	iobuf   *buf
}

// NewAllocator returns a new Slice allocator.
//
// <reserve> is the number of spare bytes to reserve at the beginning of each
// contiguous iobuf.  This can be used to reverse space for a header, for
// example.
func NewAllocator(pool *Pool, reserve uint) *Allocator {
	return &Allocator{pool: pool, reserve: reserve, index: reserve}
}

// Release releases the allocator.
func (a *Allocator) Release() {
	if a.iobuf != nil {
		a.iobuf.release()
		a.iobuf = nil
	}
	a.pool = nil
}

// Alloc allocates a new Slice.
func (a *Allocator) Alloc(bytes uint) *Slice {
	if a.iobuf == nil {
		if a.pool == nil {
			vlog.Info("iobuf.Allocator has already been closed")
			return nil
		}
		a.iobuf = a.pool.alloc(a.reserve + bytes)
	}
	if uint(len(a.iobuf.Contents))-a.index < bytes {
		a.allocIOBUF(bytes)
	}
	base := a.index
	free := base
	if free == a.reserve {
		free = 0
	}
	a.index += uint(bytes)
	return a.iobuf.slice(free, base, a.index)
}

// Copy allocates a Slice and copies the buf into it.
func (a *Allocator) Copy(buf []byte) *Slice {
	slice := a.Alloc(uint(len(buf)))
	copy(slice.Contents, buf)
	return slice
}

// allocIOBUF replaces the current iobuf with a new one that has at least
// <bytes> of storage.
func (a *Allocator) allocIOBUF(bytes uint) {
	a.iobuf.release()
	a.iobuf = a.pool.alloc(bytes + a.reserve)
	a.index = a.reserve
}
