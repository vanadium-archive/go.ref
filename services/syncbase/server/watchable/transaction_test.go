// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

// With Memstore, TestReadWriteRandom is slow with ManagedPrefixes=nil since
// every watchable.Store.Get() takes a snapshot, and memstore snapshots are
// relatively expensive since the entire data map is copied. LevelDB snapshots
// are cheap, so with LevelDB ManagedPrefixes=nil is still reasonably fast.
const useMemstoreForTest = false

type testData struct {
	key       string
	createVal string
	updateVal string
}

var data1 testData = testData{
	key:       "key-a",
	createVal: "val-a1",
	updateVal: "val-a2",
}

var data2 testData = testData{
	key:       "key-b",
	createVal: "val-b1",
	updateVal: "val-b2",
}

func TestLogEntryTimestamps(t *testing.T) {
	stImpl, destroy := createStore(useMemstoreForTest)
	defer destroy()
	t1 := time.Now()
	inc := time.Duration(1) * time.Second
	var mockClock *MockSystemClock = NewMockSystemClock(t1, inc)

	wst1, err := Wrap(stImpl, &Options{ManagedPrefixes: nil})
	if err != nil {
		t.Errorf("Failed to wrap store for create")
	}
	seqForCreate := getSeq(wst1)
	setMockSystemClock(wst1, mockClock)

	// Create data in store
	if err := store.RunInTransaction(wst1, func(st store.StoreReadWriter) error {
		// add data1
		if err := st.Put([]byte(data1.key), []byte(data1.createVal)); err != nil {
			return fmt.Errorf("can't put {%q: %v}: %v",
				data1.key, data1.createVal, err)
		}
		// add data2
		if err := st.Put([]byte(data2.key), []byte(data2.createVal)); err != nil {
			return fmt.Errorf("can't put {%q: %v}: %v",
				data2.key, data2.createVal, err)
		}
		return nil
	}); err != nil {
		panic(fmt.Errorf("can't commit transaction: %v", err))
	}

	// read and verify LogEntries written as part of above transaction
	// We expect 2 entries in the log for the two puts.
	// Timestamp from mockclock for the commit should be t1
	verifyCommitLog(t, stImpl, seqForCreate, 2, t1)

	// Update data already present in store with a new watchable store
	wst2, err := Wrap(stImpl, &Options{ManagedPrefixes: nil})
	setMockSystemClock(wst2, mockClock)
	if err != nil {
		t.Errorf("Failed to wrap store for update")
	}
	seqForUpdate := getSeq(wst2)
	if seqForUpdate != (seqForCreate + 1) {
		t.Errorf("unexpected sequence number for update. seq for create: %d, seq for update: %d", seqForCreate, seqForUpdate)
	}

	if err := store.RunInTransaction(wst2, func(st store.StoreReadWriter) error {
		if err := checkAndUpdate(st, data1); err != nil {
			return err
		}
		if err := checkAndUpdate(st, data2); err != nil {
			return err
		}
		return nil
	}); err != nil {
		panic(fmt.Errorf("can't commit transaction: %v", err))
	}

	// read and verify LogEntries written as part of above transaction
	// We expect 4 entries in the log for the two gets and two puts.
	// Timestamp from mockclock for the commit should be t1 + 1 sec
	t2 := t1.Add(inc)
	verifyCommitLog(t, stImpl, seqForUpdate, 4, t2)
}

func checkAndUpdate(st store.StoreReadWriter, data testData) error {
	// check and update data1
	keyBytes := []byte(data.key)
	val, err := st.Get(keyBytes, nil)
	if err != nil {
		return fmt.Errorf("can't get key %q: %v", data.key, err)
	}
	if !bytes.Equal(val, []byte(data.createVal)) {
		return fmt.Errorf("Unexpected value for key %q: %q", data.key, string(val))
	}
	if err := st.Put(keyBytes, []byte(data.updateVal)); err != nil {
		return fmt.Errorf("can't put {%q: %v}: %v",
			data.key, data.updateVal, err)
	}
	return nil
}

func verifyCommitLog(t *testing.T, st store.Store, seq uint64, expectedEntries int, expectedTimestamp time.Time) {
	var ler *LogEntryReader = NewLogEntryReader(st, seq)
	var entryCount int = 0
	for ler.Advance() {
		_, entry := ler.GetEntry()
		entryCount++
		if entry.CommitTimestamp != expectedTimestamp.UnixNano() {
			errStr := "Unexpected timestamp found for entry." +
				" Expected: %d, found: %d"
			t.Errorf(errStr, expectedTimestamp.UnixNano(), entry.CommitTimestamp)
		}
	}
	if entryCount != expectedEntries {
		t.Errorf("Unexpected number of log entries found. Expected: %d, found: %d", expectedEntries, entryCount)
	}
}
