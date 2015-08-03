// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package store

import (
	"v.io/v23/verror"
)

type SnapshotSpecImpl struct{}

func (s *SnapshotSpecImpl) __snapshotSpec() {}

// RunInTransaction runs the given fn in a transaction, managing retries and
// commit/abort.
func RunInTransaction(st Store, fn func(tx Transaction) error) error {
	// TODO(rogulenko): Make the number of attempts configurable.
	// TODO(rogulenko): Change the default number of attempts to 3. Currently,
	// some storage engine tests fail when the number of attempts is that low.
	var err error
	for i := 0; i < 100; i++ {
		// TODO(sadovsky): Should NewTransaction return an error? If not, how will
		// we deal with RPC errors when talking to remote storage engines? (Note,
		// client-side BeginBatch returns an error.)
		tx := st.NewTransaction()
		if err = fn(tx); err != nil {
			tx.Abort()
			return err
		}
		// TODO(sadovsky): Commit() can fail for a number of reasons, e.g. RPC
		// failure or ErrConcurrentTransaction. Depending on the cause of failure,
		// it may be desirable to retry the Commit() and/or to call Abort().
		if err = tx.Commit(); verror.ErrorID(err) != ErrConcurrentTransaction.ID {
			return err
		}
	}
	return err
}

// CopyBytes copies elements from a source slice into a destination slice.
// The returned slice may be a sub-slice of dst if dst was large enough to hold
// src. Otherwise, a newly allocated slice will be returned.
// TODO(rogulenko): add some tests.
func CopyBytes(dst, src []byte) []byte {
	if cap(dst) < len(src) {
		newlen := cap(dst)*2 + 2
		if newlen < len(src) {
			newlen = len(src)
		}
		dst = make([]byte, newlen)
	}
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}

//////////////////////////////////////////////////////////////
// Read and Write types used for storing transcation reads
// and uncommitted writes.

type ScanRange struct {
	Start, Limit []byte
}

type ReadSet struct {
	Keys   [][]byte
	Ranges []ScanRange
}

type WriteType int

const (
	PutOp WriteType = iota
	DeleteOp
)

type WriteOp struct {
	T     WriteType
	Key   []byte
	Value []byte
}

type WriteOpArray []WriteOp

func (a WriteOpArray) Len() int {
	return len(a)
}

func (a WriteOpArray) Less(i, j int) bool {
	return string(a[i].Key) < string(a[j].Key)
}

func (a WriteOpArray) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
