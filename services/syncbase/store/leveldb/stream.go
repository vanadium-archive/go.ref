// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
// #include "syncbase_leveldb.h"
import "C"
import (
	"bytes"
	"errors"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

// stream is a wrapper around LevelDB iterator that implements
// the store.Stream interface.
// TODO(rogulenko): ensure thread safety.
type stream struct {
	cIter *C.syncbase_leveldb_iterator_t
	end   []byte

	hasAdvanced bool
	err         error
}

var _ store.Stream = (*stream)(nil)

func newStream(d *db, start, end []byte, cOpts *C.leveldb_readoptions_t) *stream {
	cStr, size := cSlice(start)
	cIter := C.syncbase_leveldb_create_iterator(d.cDb, cOpts, cStr, size)
	return &stream{
		cIter: cIter,
		end:   end,
	}
}

func (it *stream) destroyLeveldbIter() {
	C.syncbase_leveldb_iter_destroy(it.cIter)
	it.cIter = nil
}

// Advance implements the store.Stream interface.
func (it *stream) Advance() bool {
	if it.cIter == nil {
		return false
	}
	// The C iterator starts out initialized, pointing at the first value; we
	// shouldn't move it during the first Advance() call.
	if !it.hasAdvanced {
		it.hasAdvanced = true
	} else {
		C.syncbase_leveldb_iter_next(it.cIter)
	}
	if it.cIter.is_valid != 0 && bytes.Compare(it.end, it.cKey()) > 0 {
		return true
	}

	var cError *C.char
	C.syncbase_leveldb_iter_get_error(it.cIter, &cError)
	it.err = goError(cError)
	it.destroyLeveldbIter()
	return false
}

// Key implements the store.Stream interface.
func (it *stream) Key(keybuf []byte) []byte {
	if !it.hasAdvanced {
		panic("stream has never been advanced")
	}
	if it.cIter == nil {
		panic("illegal state")
	}
	return store.CopyBytes(keybuf, it.cKey())
}

// Value implements the store.Stream interface.
func (it *stream) Value(valbuf []byte) []byte {
	if !it.hasAdvanced {
		panic("stream has never been advanced")
	}
	if it.cIter == nil {
		panic("illegal state")
	}
	return store.CopyBytes(valbuf, it.cVal())
}

// Err implements the store.Stream interface.
func (it *stream) Err() error {
	return it.err
}

// Cancel implements the store.Stream interface.
func (it *stream) Cancel() {
	if it.cIter == nil {
		return
	}
	it.err = errors.New("canceled")
	it.destroyLeveldbIter()
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
