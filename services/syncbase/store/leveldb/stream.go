// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
// #include "syncbase_leveldb.h"
import "C"
import (
	"bytes"
	"sync"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

// stream is a wrapper around LevelDB iterator that implements
// the store.Stream interface.
type stream struct {
	// mu protects the state of the stream.
	mu    sync.Mutex
	cIter *C.syncbase_leveldb_iterator_t
	end   []byte

	hasAdvanced bool
	err         error

	// hasValue is true iff a value has been staged. If hasValue is true,
	// key and value point to the staged key/value pair. The underlying buffers
	// of key and value are allocated on the C heap until Cancel is called,
	// at which point they are copied to the Go heap.
	hasValue bool
	key      []byte
	value    []byte
}

var _ store.Stream = (*stream)(nil)

func newStream(d *db, start, end []byte, cOpts *C.leveldb_readoptions_t) *stream {
	cStr, size := cSlice(start)
	// TODO(rogulenko): check if (db.cDb != nil) under a db-scoped mutex.
	cIter := C.syncbase_leveldb_create_iterator(d.cDb, cOpts, cStr, size)
	return &stream{
		cIter: cIter,
		end:   end,
	}
}

func (s *stream) destroyLeveldbIter() {
	C.syncbase_leveldb_iter_destroy(s.cIter)
	s.cIter = nil
}

// Advance implements the store.Stream interface.
func (s *stream) Advance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hasValue = false
	if s.cIter == nil {
		return false
	}
	// The C iterator starts out initialized, pointing at the first value; we
	// shouldn't move it during the first Advance() call.
	if !s.hasAdvanced {
		s.hasAdvanced = true
	} else {
		C.syncbase_leveldb_iter_next(s.cIter)
	}
	if s.cIter.is_valid != 0 && bytes.Compare(s.end, s.cKey()) > 0 {
		s.hasValue = true
		s.key = s.cKey()
		s.value = s.cVal()
		return true
	}

	var cError *C.char
	C.syncbase_leveldb_iter_get_error(s.cIter, &cError)
	s.err = goError(cError)
	s.destroyLeveldbIter()
	return false
}

// Key implements the store.Stream interface.
func (s *stream) Key(keybuf []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasValue {
		panic("nothing staged")
	}
	return store.CopyBytes(keybuf, s.key)
}

// Value implements the store.Stream interface.
func (s *stream) Value(valbuf []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasValue {
		panic("nothing staged")
	}
	return store.CopyBytes(valbuf, s.value)
}

// Err implements the store.Stream interface.
func (s *stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Cancel implements the store.Stream interface.
// TODO(rogulenko): make Cancel non-blocking.
func (s *stream) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cIter == nil {
		return
	}
	s.err = verror.New(verror.ErrCanceled, nil)
	// s.hasValue might be false if Advance was never called.
	if s.hasValue {
		// We copy the key and the value from the C heap to the Go heap before
		// deallocating the C iterator.
		s.key = store.CopyBytes(nil, s.cKey())
		s.value = store.CopyBytes(nil, s.cVal())
	}
	s.destroyLeveldbIter()
}

// cKey returns the current key.
// The returned []byte points to a buffer allocated on the C heap. This buffer
// is valid until the next call to Advance or Cancel.
func (it *stream) cKey() []byte {
	return goBytes(it.cIter.key, it.cIter.key_len)
}

// cVal returns the current value.
// The returned []byte points to a buffer allocated on the C heap. This buffer
// is valid until the next call to Advance or Cancel.
func (it *stream) cVal() []byte {
	return goBytes(it.cIter.val, it.cIter.val_len)
}
