// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gkv provides a simple implementation of server.Store that uses
// gkvlite for persistence. It's meant as a stopgap solution until the Syncbase
// storage engine is ready for consumption by other Vanadium modules. Since it's
// a stopgap, it doesn't bother with entry-level locking.
package gkv

import (
	"os"
	"strconv"
	"sync"

	"github.com/steveyen/gkvlite"

	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/groups/internal/store"
)

const collectionName string = "c"

type entry struct {
	Value   interface{}
	Version uint64
}

// TODO(sadovsky): Compaction.
type gkv struct {
	mu   sync.Mutex
	err  error
	file *os.File
	kvst *gkvlite.Store
	coll *gkvlite.Collection
}

var _ store.Store = (*gkv)(nil)

func New(filename string) (store.Store, error) {
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, convertError(err)
	}
	kvst, err := gkvlite.NewStore(file)
	if err != nil {
		file.Close()
		return nil, convertError(err)
	}
	res := &gkv{file: file, kvst: kvst}
	coll := kvst.GetCollection(collectionName)
	// Create collection if needed.
	if coll == nil {
		coll = kvst.SetCollection(collectionName, nil)
		if err := res.flush(); err != nil {
			res.Close()
			return nil, convertError(err)
		}
	}
	res.coll = coll
	return res, nil
}

func (st *gkv) Get(k string, v interface{}) (version string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return "", convertError(st.err)
	}
	e, err := st.get(k)
	if err != nil {
		return "", err
	}
	if err := vdl.Convert(v, e.Value); err != nil {
		return "", convertError(err)
	}
	return strconv.FormatUint(e.Version, 10), nil
}

func (st *gkv) Insert(k string, v interface{}) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return convertError(st.err)
	}
	if _, err := st.get(k); verror.ErrorID(err) != store.ErrUnknownKey.ID {
		if err != nil {
			return err
		}
		return verror.New(store.ErrKeyExists, nil, k)
	}
	return st.put(k, &entry{Value: v})
}

func (st *gkv) Update(k string, v interface{}, version string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return convertError(st.err)
	}
	e, err := st.get(k)
	if err != nil {
		return err
	}
	if err := e.checkVersion(version); err != nil {
		return err
	}
	return st.put(k, &entry{Value: v, Version: e.Version + 1})
}

func (st *gkv) Delete(k string, version string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return convertError(st.err)
	}
	e, err := st.get(k)
	if err != nil {
		return err
	}
	if err := e.checkVersion(version); err != nil {
		return err
	}
	return st.delete(k)
}

func (st *gkv) Close() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return convertError(st.err)
	}
	st.err = verror.New(verror.ErrCanceled, nil, "closed store")
	st.kvst.Close()
	return convertError(st.file.Close())
}

////////////////////////////////////////
// Internal helpers

// get, put, delete, and flush all assume st.mu is held.
func (st *gkv) get(k string) (*entry, error) {
	bytes, err := st.coll.Get([]byte(k))
	if err != nil {
		return nil, convertError(err)
	}
	if bytes == nil {
		return nil, verror.New(store.ErrUnknownKey, nil, k)
	}
	e := &entry{}
	if err := vom.Decode(bytes, e); err != nil {
		return nil, convertError(err)
	}
	return e, nil
}

func (st *gkv) put(k string, e *entry) error {
	bytes, err := vom.Encode(e)
	if err != nil {
		return convertError(err)
	}
	if err := st.coll.Set([]byte(k), bytes); err != nil {
		return convertError(err)
	}
	return convertError(st.flush())
}

func (st *gkv) delete(k string) error {
	if _, err := st.coll.Delete([]byte(k)); err != nil {
		return convertError(err)
	}
	return convertError(st.flush())
}

func (st *gkv) flush() error {
	if err := st.kvst.Flush(); err != nil {
		return convertError(err)
	}
	// TODO(sadovsky): Better handling for the case where kvst.Flush() succeeds
	// but file.Sync() fails. See discussion in v.io/c/11829.
	return convertError(st.file.Sync())
}

func (e *entry) checkVersion(version string) error {
	newVersion := strconv.FormatUint(e.Version, 10)
	if version != newVersion {
		return verror.NewErrBadVersion(nil)
	}
	return nil
}

func convertError(err error) error {
	return verror.Convert(verror.IDAction{}, nil, err)
}
