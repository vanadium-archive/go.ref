// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO(rogulenko): add more tests.

package test

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

type operation int

const (
	Put    operation = 0
	Delete operation = 1
)

type testStep struct {
	op  operation
	key int
}

func randomBytes(rnd *rand.Rand, length int) []byte {
	var res []byte
	for i := 0; i < length; i++ {
		res = append(res, '0'+byte(rnd.Intn(10)))
	}
	return res
}

// storeState is the in-memory representation of the store state.
type storeState struct {
	// We assume that the database has keys [0..size).
	size     int
	rnd      *rand.Rand
	memtable map[string][]byte
}

func newStoreState(size int) *storeState {
	return &storeState{
		size,
		rand.New(rand.NewSource(239017)),
		make(map[string][]byte),
	}
}

func (s *storeState) clone() *storeState {
	other := &storeState{
		s.size,
		s.rnd,
		make(map[string][]byte),
	}
	for k, v := range s.memtable {
		other.memtable[k] = v
	}
	return other
}

// nextKey returns the smallest key in the store that is not less than the
// provided key. If there is no such key, returns size.
func (s *storeState) lowerBound(key int) int {
	for key < s.size {
		if _, ok := s.memtable[fmt.Sprintf("%05d", key)]; ok {
			return key
		}
		key++
	}
	return key
}

// verify checks that various read operations on store.Store and memtable return
// the same results.
func (s *storeState) verify(t *testing.T, st store.StoreReader) {
	var key, value []byte
	var err error
	// Verify Get().
	for i := 0; i < s.size; i++ {
		keystr := fmt.Sprintf("%05d", i)
		answer, ok := s.memtable[keystr]
		key = []byte(keystr)
		value, err = st.Get(key, value)
		if ok {
			if err != nil || !bytes.Equal(value, answer) {
				t.Fatalf("unexpected get result for %q: got {%q, %v}, want {%q, nil}", keystr, value, err, answer)
			}
		} else {
			if !reflect.DeepEqual(&store.ErrUnknownKey{Key: keystr}, err) {
				t.Fatalf("unexpected get error for key %q: %v", keystr, err)
			}
		}
	}
	// Verify 10 random Scan() calls.
	for i := 0; i < 10; i++ {
		start, end := s.rnd.Intn(s.size), s.rnd.Intn(s.size)
		if start > end {
			start, end = end, start
		}
		end++
		stream, err := st.Scan([]byte(fmt.Sprintf("%05d", start)), []byte(fmt.Sprintf("%05d", end)))
		if err != nil {
			t.Fatalf("can't create stream: %v", err)
		}
		for stream.Advance() {
			start = s.lowerBound(start)
			keystr := fmt.Sprintf("%05d", start)
			key, value = stream.Key(key), stream.Value(value)
			if string(key) != keystr {
				t.Fatalf("unexpected key during scan: got %q, want %q", key, keystr)
			}
			if !bytes.Equal(value, s.memtable[keystr]) {
				t.Fatalf("unexpected value during scan: got %q, want %q", value, s.memtable[keystr])
			}
			start++
		}
		if start = s.lowerBound(start); start < end {
			t.Fatalf("stream ended unexpectedly")
		}
	}
}

// runReadWriteTest verifies read/write/snapshot operations.
func runReadWriteTest(t *testing.T, st store.Store, size int, steps []testStep) {
	s := newStoreState(size)
	// We verify database state no more than ~100 times to prevent the test from
	// being slow.
	frequency := (len(steps) + 99) / 100
	var states []*storeState
	var snapshots []store.Snapshot
	for i, step := range steps {
		if step.key < 0 || step.key >= s.size {
			t.Fatalf("invalid test step %v", step)
		}
		switch step.op {
		case Put:
			key := fmt.Sprintf("%05d", step.key)
			value := randomBytes(s.rnd, 100)
			s.memtable[key] = value
			st.Put([]byte(key), value)
		case Delete:
			key := fmt.Sprintf("%05d", step.key)
			if _, ok := s.memtable[key]; ok {
				delete(s.memtable, key)
				st.Delete([]byte(key))
			}
		default:
			t.Fatalf("invalid test step %v", step)
		}
		if i%frequency == 0 {
			s.verify(t, st)
			states = append(states, s.clone())
			snapshots = append(snapshots, st.NewSnapshot())
		}
	}
	s.verify(t, st)
	for i := 0; i < len(states); i++ {
		states[i].verify(t, snapshots[i])
		snapshots[i].Close()
	}
}

// RunReadWriteBasicTest runs a basic test that verifies reads, writes and
// snapshots.
func RunReadWriteBasicTest(t *testing.T, st store.Store) {
	runReadWriteTest(t, st, 3, []testStep{
		testStep{Put, 1},
		testStep{Put, 2},
		testStep{Delete, 1},
		testStep{Put, 1},
		testStep{Put, 2},
	})
}

// RunReadWriteRandomTest runs a random-generated test that verifies reads,
// writes and snapshots.
func RunReadWriteRandomTest(t *testing.T, st store.Store) {
	rnd := rand.New(rand.NewSource(239017))
	var testcase []testStep
	size := 50
	for i := 0; i < 10000; i++ {
		testcase = append(testcase, testStep{operation(rnd.Intn(2)), rnd.Intn(size)})
	}
	runReadWriteTest(t, st, size, testcase)
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
