// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"runtime"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store/test"
)

func init() {
	runtime.GOMAXPROCS(10)
}

func TestReadWriteBasic(t *testing.T) {
	st := New()
	defer st.Close()
	test.RunReadWriteBasicTest(t, st)
}

func TestReadWriteRandom(t *testing.T) {
	st := New()
	defer st.Close()
	test.RunReadWriteRandomTest(t, st)
}

func TestTransactionsWithGet(t *testing.T) {
	st := New()
	defer st.Close()
	// TODO(sadovsky): Enable this test once we've added a retry loop to
	// RunInTransaction. Without that, concurrency makes the test fail.
	//test.RunTransactionsWithGetTest(t, st)
}
