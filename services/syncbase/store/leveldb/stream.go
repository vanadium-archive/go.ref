// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
// #include "syncbase_leveldb.h"
import "C"
import (
	"errors"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/x/lib/vlog"
)

// stream is a wrapper around LevelDB iterator that implements
// the store.Stream interface.
// TODO(rogulenko): ensure thread safety.
type stream struct {
	cIter *C.syncbase_leveldb_iterator_t
	end   string

	advancedOnce bool
	err          error
}

var _ store.Stream = (*stream)(nil)

func newStream(db *DB, start, end string, cOpts *C.leveldb_readoptions_t) *stream {
	cStr, size := cSlice(start)
	cIter := C.syncbase_leveldb_create_iterator(db.cDb, cOpts, cStr, size)
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
	// C iterator is already initialized after creation; we shouldn't move
	// it during first Advance() call.
	if !it.advancedOnce {
		it.advancedOnce = true
	} else {
		C.syncbase_leveldb_iter_next(it.cIter)
	}
	if it.cIter.is_valid != 0 && it.end > it.key() {
		return true
	}

	var cError *C.char
	C.syncbase_leveldb_iter_get_error(it.cIter, &cError)
	it.err = goError(cError)
	it.destroyLeveldbIter()
	return false
}

// Value implements the store.Stream interface.
// The returned values point to buffers allocated on C heap.
// The data is valid until the next call to Advance or Cancel.
func (it *stream) Value() store.KeyValue {
	if !it.advancedOnce {
		vlog.Fatal("stream has never been advanced")
	}
	if it.cIter == nil {
		vlog.Fatal("illegal state")
	}
	return store.KeyValue{
		Key:   it.key(),
		Value: it.val(),
	}
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
	it.err = errors.New("cancelled")
	it.destroyLeveldbIter()
}

// key returns the key. The key points to buffer allocated on C heap.
// The data is valid until the next call to Advance or Cancel.
func (it *stream) key() string {
	return goString(it.cIter.key, it.cIter.key_len)
}

// val returns the value. The value points to buffer allocated on C heap.
// The data is valid until the next call to Advance or Cancel.
func (it *stream) val() []byte {
	return goBytes(it.cIter.val, it.cIter.val_len)
}
