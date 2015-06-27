// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

// TODO(sadovsky): Avoid copying back and forth between []byte's and strings.
// We should probably convert incoming strings to []byte's as early as possible,
// and deal exclusively in []byte's internally.

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/verror"
)

var (
	rng     *rand.Rand = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	rngLock sync.Mutex
)

func makeVersionKey(key []byte) []byte {
	return []byte(join(util.VersionPrefix, string(key)))
}

func makeAtVersionKey(key, version []byte) []byte {
	return []byte(join(string(key), string(version)))
}

// GetVersion returns the current version of a managed key. This method is used
// by the Sync module when the initiator is attempting to add new versions of
// objects. Reading the version key is used for optimistic concurrency control.
func GetVersion(ctx *context.T, st store.StoreReader, key []byte) ([]byte, error) {
	wtx := st.(*transaction)

	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return nil, convertError(wtx.err)
	}
	return getVersion(wtx.itx, key)
}

// GetAtVersion returns the value of a managed key at the requested
// version. This method is used by the Sync module when the responder needs to
// send objects over the wire.
func GetAtVersion(ctx *context.T, st store.StoreReader, key, valbuf, version []byte) ([]byte, error) {
	wtx := st.(*transaction)

	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return valbuf, convertError(wtx.err)
	}
	return getAtVersion(wtx.itx, key, valbuf, version)
}

func getVersion(st store.StoreReader, key []byte) ([]byte, error) {
	return st.Get(makeVersionKey(key), nil)
}

func getAtVersion(st store.StoreReader, key, valbuf, version []byte) ([]byte, error) {
	return st.Get(makeAtVersionKey(key, version), valbuf)
}

func getVersioned(st store.StoreReader, key, valbuf []byte) ([]byte, error) {
	checkTransactionOrSnapshot(st)
	version, err := getVersion(st, key)
	if err != nil {
		return valbuf, err
	}
	return getAtVersion(st, key, valbuf, version)
}

// PutAtVersion puts a value for the managed key at the requested version. This
// method is used by the Sync module exclusively when the initiator adds objects
// with versions created on other Syncbases.
func PutAtVersion(ctx *context.T, tx store.Transaction, key, valbuf, version []byte) error {
	wtx := tx.(*transaction)

	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}

	return wtx.itx.Put(makeAtVersionKey(key, version), valbuf)
}

// PutVersion updates the version of a managed key to the requested
// version. This method is used by the Sync module exclusively when the
// initiator selects which of the already stored versions (via PutAtVersion
// calls) becomes the current version.
func PutVersion(ctx *context.T, tx store.Transaction, key, version []byte) error {
	wtx := tx.(*transaction)

	wtx.mu.Lock()
	defer wtx.mu.Unlock()
	if wtx.err != nil {
		return convertError(wtx.err)
	}

	if err := wtx.itx.Put(makeVersionKey(key), version); err != nil {
		return err
	}
	wtx.ops = append(wtx.ops, &OpPut{PutOp{Key: key, Value: version}})
	return nil
}

func putVersioned(tx store.Transaction, key, value []byte) error {
	rngLock.Lock()
	num := rng.Int63()
	rngLock.Unlock()

	version := []byte(fmt.Sprintf("%x", num))
	if err := tx.Put(makeVersionKey(key), version); err != nil {
		return err
	}
	return tx.Put(makeAtVersionKey(key, version), value)
}

func deleteVersioned(tx store.Transaction, key []byte) error {
	return tx.Delete(makeVersionKey(key))
}

func checkTransactionOrSnapshot(st store.StoreReader) {
	_, isTransaction := st.(store.Transaction)
	_, isSnapshot := st.(store.Snapshot)
	if !isTransaction && !isSnapshot {
		panic("neither a Transaction nor a Snapshot")
	}
}

func join(parts ...string) string {
	return util.JoinKeyParts(parts...)
}

func split(key string) []string {
	return util.SplitKeyParts(key)
}

func convertError(err error) error {
	return verror.Convert(verror.IDAction{}, nil, err)
}
