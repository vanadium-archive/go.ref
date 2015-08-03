// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

// TODO(sadovsky): Avoid copying back and forth between []byte's and strings.
// We should probably convert incoming strings to []byte's as early as possible,
// and deal exclusively in []byte's internally.
// TODO(rdaoud): I propose we standardize on key and version being strings and
// the value being []byte within Syncbase.  We define invalid characters in the
// key space (and reserve "$" and ":").  The lower storage engine layers are
// free to map that to what they need internally ([]byte or string).

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

var (
	rng     *rand.Rand = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	rngLock sync.Mutex
)

// NewVersion returns a new version for a store entry mutation.
func NewVersion() []byte {
	// TODO(rdaoud): revisit the number of bits: should we use 128 bits?
	// Note: the version has to be unique per object key, not on its own.
	// TODO(rdaoud): move sync's rand64() to a general Syncbase spot and
	// reuse it here.
	rngLock.Lock()
	num := rng.Int63()
	rngLock.Unlock()

	return []byte(fmt.Sprintf("%x", num))
}

func makeVersionKey(key []byte) []byte {
	return []byte(join(util.VersionPrefix, string(key)))
}

func makeAtVersionKey(key, version []byte) []byte {
	return []byte(join(string(key), string(version)))
}

func getVersion(sntx store.SnapshotOrTransaction, key []byte) ([]byte, error) {
	return sntx.Get(makeVersionKey(key), nil)
}

func getAtVersion(st store.StoreReader, key, valbuf, version []byte) ([]byte, error) {
	return st.Get(makeAtVersionKey(key, version), valbuf)
}

func getVersioned(sntx store.SnapshotOrTransaction, key, valbuf []byte) ([]byte, error) {
	version, err := getVersion(sntx, key)
	if err != nil {
		return valbuf, err
	}
	return getAtVersion(sntx, key, valbuf, version)
}

func putVersioned(tx store.Transaction, key, value []byte) ([]byte, error) {
	version := NewVersion()
	if err := tx.Put(makeVersionKey(key), version); err != nil {
		return nil, err
	}
	if err := tx.Put(makeAtVersionKey(key, version), value); err != nil {
		return nil, err
	}
	return version, nil
}

func deleteVersioned(tx store.Transaction, key []byte) error {
	return tx.Delete(makeVersionKey(key))
}

func getLogEntryKey(seq uint64) string {
	// Note: MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	return join(util.LogPrefix, fmt.Sprintf("%016x", seq))
}

// logEntryExists returns true iff the log contains an entry with the given
// sequence number.
func logEntryExists(st store.StoreReader, seq uint64) (bool, error) {
	_, err := st.Get([]byte(getLogEntryKey(seq)), nil)
	if err != nil && verror.ErrorID(err) != store.ErrUnknownKey.ID {
		return false, err
	}
	return err == nil, nil
}

// getNextLogSeq returns the next sequence number to be used for a new commit.
// NOTE: this function assumes that all sequence numbers in the log represent
// some range [start, limit] without gaps.
func getNextLogSeq(st store.StoreReader) (uint64, error) {
	// Determine initial value for seq.
	// TODO(sadovsky): Consider using a bigger seq.

	// Find the beginning of the log.
	it := st.Scan(util.ScanPrefixArgs(util.LogPrefix, ""))
	if !it.Advance() {
		return 0, nil
	}
	if it.Err() != nil {
		return 0, it.Err()
	}
	key := string(it.Key(nil))
	parts := split(key)
	if len(parts) != 2 {
		panic("wrong number of parts: " + key)
	}
	seq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		panic("failed to parse seq: " + key)
	}
	var step uint64 = 1
	// Suppose the actual value we are looking for is S. First, we estimate the
	// range for S. We find seq, step: seq < S <= seq + step.
	for {
		if ok, err := logEntryExists(st, seq+step); err != nil {
			return 0, err
		} else if !ok {
			break
		}
		seq += step
		step *= 2
	}
	// Next we keep the seq < S <= seq + step invariant, reducing step to 1.
	for step > 1 {
		step /= 2
		if ok, err := logEntryExists(st, seq+step); err != nil {
			return 0, err
		} else if ok {
			seq += step
		}
	}
	// Now seq < S <= seq + 1, thus S = seq + 1.
	return seq + 1, nil
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
