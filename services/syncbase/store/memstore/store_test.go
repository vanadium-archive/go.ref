// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"runtime"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/test"
)

func init() {
	runtime.GOMAXPROCS(10)
}

func TestStream(t *testing.T) {
	runTest(t, test.RunStreamTest)
}

func TestSnapshot(t *testing.T) {
	runTest(t, test.RunSnapshotTest)
}

// TODO(rogulenko): Enable this test once memstore.Close causes memstore to
// disallow subsequent operations.
/*
func TestStoreState(t *testing.T) {
	runTest(t, test.RunStoreStateTest)
}

func TestClose(t *testing.T) {
	runTest(t, test.RunCloseTest)
}
*/

func TestReadWriteBasic(t *testing.T) {
	runTest(t, test.RunReadWriteBasicTest)
}

func TestReadWriteRandom(t *testing.T) {
	runTest(t, test.RunReadWriteRandomTest)
}

func TestTransactionState(t *testing.T) {
	runTest(t, test.RunTransactionStateTest)
}

func TestTransactionsWithGet(t *testing.T) {
	// TODO(sadovsky): Enable this test once we've added a retry loop to
	// RunInTransaction. Without that, concurrency makes the test fail.
	// runTest(t, test.RunTransactionsWithGetTest)
}

func runTest(t *testing.T, f func(t *testing.T, st store.Store)) {
	st := New()
	defer st.Close()
	f(t, st)
}
