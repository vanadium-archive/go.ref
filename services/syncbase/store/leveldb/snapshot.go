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
	db        *DB
	cSnapshot *C.leveldb_snapshot_t
	cOpts     *C.leveldb_readoptions_t
}

var _ store.Snapshot = (*snapshot)(nil)

func newSnapshot(db *DB) *snapshot {
	cSnapshot := C.leveldb_create_snapshot(db.cDb)
	cOpts := C.leveldb_readoptions_create()
	C.leveldb_readoptions_set_verify_checksums(cOpts, 1)
	C.leveldb_readoptions_set_snapshot(cOpts, cSnapshot)
	return &snapshot{
		db,
		cSnapshot,
		cOpts,
	}
}

// Close implements the store.Snapshot interface.
func (s *snapshot) Close() error {
	C.leveldb_readoptions_destroy(s.cOpts)
	C.leveldb_release_snapshot(s.db.cDb, s.cSnapshot)
	return nil
}

// Scan implements the store.StoreReader interface.
func (s *snapshot) Scan(start, end []byte) (store.Stream, error) {
	return newStream(s.db, start, end, s.cOpts), nil
}

// Get implements the store.StoreReader interface.
func (s *snapshot) Get(key, valbuf []byte) ([]byte, error) {
	return s.db.getWithOpts(key, valbuf, s.cOpts)
}
