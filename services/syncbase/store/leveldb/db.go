// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package leveldb provides a LevelDB-based implementation of store.Store.
package leveldb

// #cgo LDFLAGS: -lleveldb -lsnappy
// #include <stdlib.h>
// #include "leveldb/c.h"
// #include "syncbase_leveldb.h"
import "C"
import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/transactions"
)

// db is a wrapper around LevelDB that implements the transactions.BatchStore
// interface.
type db struct {
	// mu protects the state of the db.
	mu   sync.RWMutex
	node *store.ResourceNode
	cDb  *C.leveldb_t
	// Default read/write options.
	readOptions  *C.leveldb_readoptions_t
	writeOptions *C.leveldb_writeoptions_t
	err          error
}

const defaultMaxOpenFiles = 100

type OpenOptions struct {
	CreateIfMissing bool
	ErrorIfExists   bool
	MaxOpenFiles    int
}

// Open opens the database located at the given path.
func Open(path string, opts OpenOptions) (store.Store, error) {
	bs, err := openBatchStore(path, opts)
	if err != nil {
		return nil, err
	}
	return transactions.Wrap(bs), nil
}

// openBatchStore opens the non-transactional leveldb database that supports
// batch writes at the given path.
func openBatchStore(path string, opts OpenOptions) (*db, error) {
	var cError *C.char
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var cOptsCreateIfMissing, cOptsErrorIfExists C.uchar
	if opts.CreateIfMissing {
		cOptsCreateIfMissing = 1
	}
	if opts.ErrorIfExists {
		cOptsErrorIfExists = 1
	}

	// If max_open_files is not set, leveldb can open many files, leading to
	// "Too many open files" error, or other strange system behavior.
	// See https://github.com/vanadium/issues/issues/1253
	cOptsMaxOpenFiles := C.int(opts.MaxOpenFiles)
	if cOptsMaxOpenFiles <= 0 {
		cOptsMaxOpenFiles = defaultMaxOpenFiles
	}

	cOpts := C.leveldb_options_create()
	C.leveldb_options_set_create_if_missing(cOpts, cOptsCreateIfMissing)
	C.leveldb_options_set_error_if_exists(cOpts, cOptsErrorIfExists)
	C.leveldb_options_set_max_open_files(cOpts, cOptsMaxOpenFiles)
	C.leveldb_options_set_paranoid_checks(cOpts, 1)
	defer C.leveldb_options_destroy(cOpts)

	cDb := C.leveldb_open(cOpts, cPath, &cError)
	if err := goError(cError); err != nil {
		return nil, err
	}
	readOptions := C.leveldb_readoptions_create()
	C.leveldb_readoptions_set_verify_checksums(readOptions, 1)
	return &db{
		node:         store.NewResourceNode(),
		cDb:          cDb,
		readOptions:  readOptions,
		writeOptions: C.leveldb_writeoptions_create(),
	}, nil
}

// Close implements the store.Store interface.
func (d *db) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return store.ConvertError(d.err)
	}
	d.node.Close()
	C.leveldb_close(d.cDb)
	d.cDb = nil
	C.leveldb_readoptions_destroy(d.readOptions)
	d.readOptions = nil
	C.leveldb_writeoptions_destroy(d.writeOptions)
	d.writeOptions = nil
	d.err = verror.New(verror.ErrCanceled, nil, store.ErrMsgClosedStore)
	return nil
}

// Destroy removes all physical data of the database located at the given path.
func Destroy(path string) error {
	var cError *C.char
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cOpts := C.leveldb_options_create()
	defer C.leveldb_options_destroy(cOpts)
	C.leveldb_destroy_db(cOpts, cPath, &cError)
	return goError(cError)
}

// Get implements the store.StoreReader interface.
func (d *db) Get(key, valbuf []byte) ([]byte, error) {
	return d.getWithOpts(key, valbuf, d.readOptions)
}

// Scan implements the store.StoreReader interface.
func (d *db) Scan(start, limit []byte) store.Stream {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return &store.InvalidStream{Error: d.err}
	}
	return newStream(d, d.node, start, limit, d.readOptions)
}

// NewSnapshot implements the store.Store interface.
func (d *db) NewSnapshot() store.Snapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return &store.InvalidSnapshot{Error: d.err}
	}
	return newSnapshot(d, d.node)
}

// WriteBatch implements the transactions.BatchStore interface.
func (d *db) WriteBatch(batch ...transactions.WriteOp) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	cBatch := C.leveldb_writebatch_create()
	defer C.leveldb_writebatch_destroy(cBatch)
	for _, write := range batch {
		switch write.T {
		case transactions.PutOp:
			cKey, cKeyLen := cSlice(write.Key)
			cVal, cValLen := cSlice(write.Value)
			C.leveldb_writebatch_put(cBatch, cKey, cKeyLen, cVal, cValLen)
		case transactions.DeleteOp:
			cKey, cKeyLen := cSlice(write.Key)
			C.leveldb_writebatch_delete(cBatch, cKey, cKeyLen)
		default:
			panic(fmt.Sprintf("unknown write operation type: %v", write.T))
		}
	}
	var cError *C.char
	C.leveldb_write(d.cDb, d.writeOptions, cBatch, &cError)
	return goError(cError)
}

// getWithOpts returns the value for the given key.
// cOpts may contain a pointer to a snapshot.
func (d *db) getWithOpts(key, valbuf []byte, cOpts *C.leveldb_readoptions_t) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.err != nil {
		return valbuf, store.ConvertError(d.err)
	}
	var cError *C.char
	var valLen C.size_t
	cStr, cLen := cSlice(key)
	val := C.leveldb_get(d.cDb, cOpts, cStr, cLen, &valLen, &cError)
	if err := goError(cError); err != nil {
		return valbuf, err
	}
	if val == nil {
		return valbuf, verror.New(store.ErrUnknownKey, nil, string(key))
	}
	defer C.leveldb_free(unsafe.Pointer(val))
	return store.CopyBytes(valbuf, goBytes(val, valLen)), nil
}

// filesystemBytes returns the amount of filesystem bytes used by "level"
// files. For small databases this call is about 3X faster than a Get on the
// database; for large databases it is two orders of magnitude faster than a
// Get.
func (d *db) filesystemBytes() uint64 {
	return uint64(C.syncbase_leveldb_filesystem_bytes(d.cDb))
}

func (d *db) property(propName string) string {
	propNameC := C.CString(propName)
	defer C.free(unsafe.Pointer(propNameC))
	value := C.leveldb_property_value(d.cDb, propNameC)
	if value == nil {
		return ""
	}
	defer C.leveldb_free(unsafe.Pointer(value))
	return C.GoString(value)
}

// stats returns per-level statistics as a set of parallel slices of the same
// length.  For small databases this call is about 2X slower than a Get; for
// large databases it is about one order of magnitude faster than a Get.
func (d *db) stats() (fileCounts, fileMBs, readMBs, writeMBs []int, err error) {
	// It's unfortunate that LevelDB only provides the stats in a formatted
	// string, so we have to parse them out.
	text := d.property("leveldb.stats")

	// Expecting text that looks like this:
	//
	//                               Compactions
	//Level  Files Size(MB) Time(sec) Read(MB) Write(MB)
	//--------------------------------------------------
	//  0        8       32         1        0        64
	//  1        9       36         1       20        44
	//  2       12       24         1       24        28

	lines := strings.Split(text, "\n")
	inHeader := true
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if !inHeader {
			var n, level, fCount, fMB, t, rMB, wMb int
			n, err = fmt.Sscan(line, &level, &fCount, &fMB, &t, &rMB, &wMb)
			if n != 6 {
				err = fmt.Errorf(`Read %d stats items from "%s": %v`,
					n, line, err)
				return
			}
			fileCounts = append(fileCounts, fCount)
			fileMBs = append(fileMBs, fMB)
			readMBs = append(readMBs, rMB)
			writeMBs = append(writeMBs, wMb)
		}
		if inHeader && strings.HasPrefix(line, "--------------") {
			inHeader = false
		}
	}
	return
}

// fileCount eturns the number of "level" files.  The performance of this is
// constant for different sizes databases, and is about the same as the
// performance of a Get on a small database.
func (d *db) fileCount() int {
	return int(C.syncbase_leveldb_file_count(d.cDb))
}
