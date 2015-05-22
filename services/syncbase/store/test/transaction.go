// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/verror"
)

// RunTransactionTest verifies operations that modify the state of a
// store.Transaction.
func RunTransactionStateTest(t *testing.T, st store.Store) {
	abortFunctions := []func(t *testing.T, tx store.Transaction) (string, verror.ID){
		func(t *testing.T, tx store.Transaction) (string, verror.ID) {
			if err := tx.Abort(); err != nil {
				Fatalf(t, "can't abort the transaction: %v", err)
			}
			return "aborted transaction", verror.ErrCanceled.ID
		},
		func(t *testing.T, tx store.Transaction) (string, verror.ID) {
			if err := tx.Commit(); err != nil {
				Fatalf(t, "can't commit the transaction: %v", err)
			}
			return "committed transaction", verror.ErrBadState.ID
		},
	}
	for _, fn := range abortFunctions {
		key1, value1 := []byte("key1"), []byte("value1")
		st.Put(key1, value1)
		key2 := []byte("key2")
		tx := st.NewTransaction()

		// Test Get and Scan.
		verifyGet(t, tx, key1, value1)
		verifyGet(t, tx, key2, nil)
		s := tx.Scan([]byte("a"), []byte("z"))
		verifyAdvance(t, s, key1, value1)
		verifyAdvance(t, s, nil, nil)

		// Test functions after Abort.
		expectedErr, expectedID := fn(t, tx)
		verifyError(t, tx.Abort(), expectedErr, expectedID)
		verifyError(t, tx.Commit(), expectedErr, expectedID)

		s = tx.Scan([]byte("a"), []byte("z"))
		verifyAdvance(t, s, nil, nil)
		verifyError(t, s.Err(), expectedErr, expectedID)

		_, err := tx.Get(key1, nil)
		verifyError(t, err, expectedErr, expectedID)
		verifyError(t, tx.Put(key1, value1), expectedErr, expectedID)
		verifyError(t, tx.Delete(key1), expectedErr, expectedID)
	}
}

// RunTransactionsWithGetTest tests transactions that use Put and Get
// operations.
// NOTE: consider setting GOMAXPROCS to something greater than 1.
func RunTransactionsWithGetTest(t *testing.T, st store.Store) {
	// Invariant: value mapped to n is sum of values of 0..n-1.
	// Each of k transactions takes m distinct random values from 0..n-1, adds 1
	// to each and m to value mapped to n.
	// The correctness of sums is checked after all transactions have been
	// committed.
	n, m, k := 10, 3, 100
	for i := 0; i <= n; i++ {
		if err := st.Put([]byte(fmt.Sprintf("%05d", i)), []byte{'0'}); err != nil {
			t.Fatalf("can't write to database")
		}
	}
	var wg sync.WaitGroup
	wg.Add(k)
	// TODO(sadovsky): This configuration creates huge resource contention.
	// Perhaps we should add some random sleep's to reduce the contention.
	for i := 0; i < k; i++ {
		go func() {
			rnd := rand.New(rand.NewSource(239017 * int64(i)))
			perm := rnd.Perm(n)
			if err := store.RunInTransaction(st, func(st store.StoreReadWriter) error {
				for j := 0; j <= m; j++ {
					var keystr string
					if j < m {
						keystr = fmt.Sprintf("%05d", perm[j])
					} else {
						keystr = fmt.Sprintf("%05d", n)
					}
					key := []byte(keystr)
					val, err := st.Get(key, nil)
					if err != nil {
						return fmt.Errorf("can't get key %q: %v", key, err)
					}
					intValue, err := strconv.ParseInt(string(val), 10, 64)
					if err != nil {
						return fmt.Errorf("can't parse int from %q: %v", val, err)
					}
					var newValue int64
					if j < m {
						newValue = intValue + 1
					} else {
						newValue = intValue + int64(m)
					}
					if err := st.Put(key, []byte(fmt.Sprintf("%d", newValue))); err != nil {
						return fmt.Errorf("can't put {%q: %v}: %v", key, newValue, err)
					}
				}
				return nil
			}); err != nil {
				panic(fmt.Errorf("can't commit transaction: %v", err))
			}
			wg.Done()
		}()
	}
	wg.Wait()
	var sum int64
	for j := 0; j <= n; j++ {
		keystr := fmt.Sprintf("%05d", j)
		key := []byte(keystr)
		val, err := st.Get(key, nil)
		if err != nil {
			t.Fatalf("can't get key %q: %v", key, err)
		}
		intValue, err := strconv.ParseInt(string(val), 10, 64)
		if err != nil {
			t.Fatalf("can't parse int from %q: %v", val, err)
		}
		if j < n {
			sum += intValue
		} else {
			if intValue != int64(m*k) {
				t.Fatalf("invalid sum value in the database: got %d, want %d", intValue, m*k)
			}
		}
	}
	if sum != int64(m*k) {
		t.Fatalf("invalid sum of values in the database: got %d, want %d", sum, m*k)
	}
}
