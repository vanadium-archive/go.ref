// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file is intended to be C++ so that we can access C++ LevelDB interface
// directly if necessary.

#include "syncbase_leveldb.h"

extern "C" {

static void PopulateIteratorFields(syncbase_leveldb_iterator_t* iter) {
  iter->is_valid = leveldb_iter_valid(iter->rep);
  if (!iter->is_valid) {
    return;
  }
  iter->key = leveldb_iter_key(iter->rep, &iter->key_len);
  iter->val = leveldb_iter_value(iter->rep, &iter->val_len);
}

syncbase_leveldb_iterator_t* syncbase_leveldb_create_iterator(
    leveldb_t* db,
    const leveldb_readoptions_t* options,
    const char* start, size_t start_len) {
  syncbase_leveldb_iterator_t* result = new syncbase_leveldb_iterator_t;
  result->rep = leveldb_create_iterator(db, options);
  leveldb_iter_seek(result->rep, start, start_len);
  PopulateIteratorFields(result);
  return result;
}

void syncbase_leveldb_iter_destroy(syncbase_leveldb_iterator_t* iter) {
  leveldb_iter_destroy(iter->rep);
  delete iter;
}

void syncbase_leveldb_iter_next(syncbase_leveldb_iterator_t* iter) {
  leveldb_iter_next(iter->rep);
  PopulateIteratorFields(iter);
}

void syncbase_leveldb_iter_get_error(
    const syncbase_leveldb_iterator_t* iter, char** errptr) {
  leveldb_iter_get_error(iter->rep, errptr);
}

}  // end extern "C"
