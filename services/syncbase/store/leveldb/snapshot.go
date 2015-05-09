// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"v.io/syncbase/x/ref/services/syncbase/store"
)

// snapshot is a wrapper around LevelDB snapshot that implements
// the store.Snapshot interface.
type snapshot struct {
	d         *db
	cSnapshot *C.leveldb_snapshot_t
	cOpts     *C.leveldb_readoptions_t
}

var _ store.Snapshot = (*snapshot)(nil)

func newSnapshot(d *db) *snapshot {
	cSnapshot := C.leveldb_create_snapshot(d.cDb)
	cOpts := C.leveldb_readoptions_create()
	C.leveldb_readoptions_set_verify_checksums(cOpts, 1)
	C.leveldb_readoptions_set_snapshot(cOpts, cSnapshot)
	return &snapshot{
		d,
		cSnapshot,
		cOpts,
	}
}

// Close implements the store.Snapshot interface.
func (s *snapshot) Close() error {
	C.leveldb_readoptions_destroy(s.cOpts)
	C.leveldb_release_snapshot(s.d.cDb, s.cSnapshot)
	return nil
}

// Get implements the store.StoreReader interface.
func (s *snapshot) Get(key, valbuf []byte) ([]byte, error) {
	return s.d.getWithOpts(key, valbuf, s.cOpts)
}

// Scan implements the store.StoreReader interface.
func (s *snapshot) Scan(start, end []byte) (store.Stream, error) {
	return newStream(s.d, start, end, s.cOpts), nil
}
