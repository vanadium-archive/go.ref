// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Helpful wrappers to a persistent key/value (K/V) DB used by Veyron Sync.
// The current underlying DB is an in-memory map.

import (
	"bytes"
	"fmt"
	"sort"

	"v.io/v23/vom"
)

var memStore map[string]*kvdb

type kvdb struct {
	tables map[string]*kvtable
}

type kvtable struct {
	data map[string][]byte
}

// kvdbOpen opens or creates a K/V DB for the given filename and table names
// within the DB.  It returns the DB handler and handlers for each table.
func kvdbOpen(filename string, tables []string) (*kvdb, []*kvtable, error) {
	if memStore == nil {
		memStore = make(map[string]*kvdb)
	}

	db := memStore[filename]
	if db == nil {
		db = &kvdb{tables: make(map[string]*kvtable)}
		memStore[filename] = db
	}

	tbls := make([]*kvtable, len(tables))

	for i, table := range tables {
		t := db.tables[table]
		if t == nil {
			t = &kvtable{data: make(map[string][]byte)}
			db.tables[table] = t
		}
		tbls[i] = t
	}

	return db, tbls, nil
}

// close closes the given K/V DB.
func (db *kvdb) close() {
}

// flush flushes the given K/V DB to disk.
func (db *kvdb) flush() {
}

// set stores (or overwrites) the given key/value pair in the DB table.
func (t *kvtable) set(key string, value interface{}) error {
	var val bytes.Buffer
	enc, err := vom.NewEncoder(&val)
	if err != nil {
		return err
	}
	if enc.Encode(value); err != nil {
		return err
	}
	t.data[key] = val.Bytes()
	return nil
}

// create stores the given key/value pair in the DB table only if
// the key does not already exist.  Otherwise it returns an error.
func (t *kvtable) create(key string, value interface{}) error {
	if t.hasKey(key) {
		return fmt.Errorf("key %s exists", key)
	}
	return t.set(key, value)
}

// update stores the given key/value pair in the DB table only if
// the key already exists.  Otherwise it returns an error.
func (t *kvtable) update(key string, value interface{}) error {
	if !t.hasKey(key) {
		return fmt.Errorf("key %s does not exist", key)
	}
	return t.set(key, value)
}

// get retrieves the value of a key from the DB table.
func (t *kvtable) get(key string, value interface{}) error {
	val := t.data[key]
	if val == nil {
		return fmt.Errorf("entry %s not found in the K/V DB table", key)
	}
	dec, err := vom.NewDecoder(bytes.NewBuffer(val))
	if err != nil {
		return err
	}
	return dec.Decode(value)
}

// del deletes the entry in the DB table given its key.
func (t *kvtable) del(key string) error {
	delete(t.data, key)
	return nil
}

// hasKey returns true if the given key exists in the DB table.
func (t *kvtable) hasKey(key string) bool {
	val, ok := t.data[key]
	return ok && val != nil
}

// keyIter iterates over all keys in a DB table invoking the given callback
// function for each one.  The key iterator callback is passed the item key.
func (t *kvtable) keyIter(keyIterCB func(key string)) error {
	keys := make([]string, 0, len(t.data))
	for k := range t.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		keyIterCB(k)
	}
	return nil
}

// getNumKeys returns the number of keys (entries) in the DB table.
func (t *kvtable) getNumKeys() uint64 {
	return uint64(len(t.data))
}
