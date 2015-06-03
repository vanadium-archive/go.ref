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
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

var rng *rand.Rand = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))

func makeVersionKey(key []byte) []byte {
	return []byte(join(util.VersionPrefix, string(key)))
}

func makeAtVersionKey(key, version []byte) []byte {
	return []byte(join(string(key), string(version)))
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

func putVersioned(tx store.Transaction, key, value []byte) error {
	version := []byte(fmt.Sprintf("%x", rng.Int63()))
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
