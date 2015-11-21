// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime/debug"
	"testing"
	"time"

	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/vclock"
)

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

func checkAndUpdate(tx store.Transaction, data testData) error {
	// check and update data1
	keyBytes := []byte(data.key)
	val, err := tx.Get(keyBytes, nil)
	if err != nil {
		return fmt.Errorf("can't get key %q: %v", data.key, err)
	}
	if !bytes.Equal(val, []byte(data.createVal)) {
		return fmt.Errorf("Unexpected value for key %q: %q", data.key, string(val))
	}
	if err := tx.Put(keyBytes, []byte(data.updateVal)); err != nil {
		return fmt.Errorf("can't put {%q: %v}: %v", data.key, data.updateVal, err)
	}
	return nil
}

func verifyCommitLog(t *testing.T, st store.Store, seq uint64, wantNumEntries int, wantTimestamp time.Time) {
	ler := newLogEntryReader(st, seq)
	numEntries := 0
	for ler.Advance() {
		_, entry := ler.GetEntry()
		numEntries++
		if entry.CommitTimestamp != wantTimestamp.UnixNano() {
			t.Errorf("Unexpected timestamp found for entry: got %v, want %v", entry.CommitTimestamp, wantTimestamp.UnixNano())
		}
	}
	if numEntries != wantNumEntries {
		t.Errorf("Unexpected number of log entries: got %v, want %v", numEntries, wantNumEntries)
	}
}

func TestLogEntryTimestamps(t *testing.T) {
	ist, destroy := createStore()
	defer destroy()
	t1 := time.Now()
	inc := time.Second
	cl := vclock.NewVClockForTests(&mockSystemClock{time: t1, inc: inc})
	wst1, err := Wrap(ist, cl, &Options{ManagedPrefixes: nil})
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	seqForCreate := getSeq(wst1)

	// Write data1 and data2 to store.
	if err := store.RunInTransaction(wst1, func(tx store.Transaction) error {
		if err := tx.Put([]byte(data1.key), []byte(data1.createVal)); err != nil {
			return fmt.Errorf("can't put {%q: %v}: %v", data1.key, data1.createVal, err)
		}
		if err := tx.Put([]byte(data2.key), []byte(data2.createVal)); err != nil {
			return fmt.Errorf("can't put {%q: %v}: %v", data2.key, data2.createVal, err)
		}
		return nil
	}); err != nil {
		panic(fmt.Errorf("can't commit transaction: %v", err))
	}

	// Read and verify LogEntries written as part of the above transaction.
	// We expect 2 entries in the log for the 2 puts.
	// Commit timestamp from mock vclock should be t1 + inc.
	verifyCommitLog(t, ist, seqForCreate, 2, t1.Add(inc))

	// Update data already present in store with a new watchable store
	wst2, err := Wrap(ist, cl, &Options{ManagedPrefixes: nil})
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}
	seqForUpdate := getSeq(wst2)
	// We expect the sequence number to have moved by +2 for the two puts.
	if seqForUpdate != (seqForCreate + 2) {
		t.Errorf("unexpected sequence number for update. seq for create: %d, seq for update: %d", seqForCreate, seqForUpdate)
	}

	if err := store.RunInTransaction(wst2, func(tx store.Transaction) error {
		if err := checkAndUpdate(tx, data1); err != nil {
			return err
		}
		if err := checkAndUpdate(tx, data2); err != nil {
			return err
		}
		return nil
	}); err != nil {
		panic(fmt.Errorf("can't commit transaction: %v", err))
	}

	// Read and verify LogEntries written as part of the above transaction.
	// We expect 4 entries in the log for the 2 gets and 2 puts.
	// Commit timestamp from mock vclock should be t1 + 2*inc.
	verifyCommitLog(t, ist, seqForUpdate, 4, t1.Add(2*inc))
}

func eq(t *testing.T, got, want interface{}) {
	if !reflect.DeepEqual(got, want) {
		debug.PrintStack()
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOpLogConsistency(t *testing.T) {
	ist, destroy := createStore()
	defer destroy()
	t1 := time.Now()
	inc := time.Second
	// Note: NewVClockForTests calls cl.SysClock.Now() once to write the initial
	// VClockData to the store.
	cl := vclock.NewVClockForTests(&mockSystemClock{time: t1, inc: inc})
	wst, err := Wrap(ist, cl, &Options{ManagedPrefixes: nil})
	if err != nil {
		t.Fatalf("Wrap failed: %v", err)
	}

	if err := store.RunInTransaction(wst, func(tx store.Transaction) error {
		putKey, putVal := []byte("foo"), []byte("bar")
		if err := tx.Put(putKey, putVal); err != nil {
			return err
		}
		getKey := []byte("foo")
		if getVal, err := tx.Get(getKey, nil); err != nil {
			return err
		} else {
			eq(t, getVal, putVal)
		}
		start, limit := []byte("aaa"), []byte("bbb")
		tx.Scan(start, limit)
		delKey := []byte("foo")
		if err := tx.Delete(delKey); err != nil {
			return err
		}
		sgPrefixes := []string{"sga", "sgb"}
		gid := interfaces.GroupId(1234)
		if err := AddSyncgroupOp(nil, tx, gid, sgPrefixes, false); err != nil {
			return err
		}
		snKey, snVersion := []byte("aa"), []byte("123")
		if err := AddSyncSnapshotOp(nil, tx, snKey, snVersion); err != nil {
			return err
		}
		pvKey, pvVersion := []byte("pv"), []byte("456")
		if err := PutVersion(nil, tx, pvKey, pvVersion); err != nil {
			return err
		}
		for _, buf := range [][]byte{putKey, putVal, getKey, start, limit, delKey, snKey, snVersion, pvKey, pvVersion} {
			buf[0] = '#'
		}
		sgPrefixes[0] = "zebra"
		return nil
	}); err != nil {
		t.Fatalf("failed to commit txn: %v", err)
	}

	// Read first (and only) batch.
	ler := newLogEntryReader(ist, 0)
	numEntries, wantNumEntries := 0, 7
	sawPut := false
	for ler.Advance() {
		_, entry := ler.GetEntry()
		numEntries++
		switch op := entry.Op.(type) {
		case OpGet:
			eq(t, string(op.Value.Key), "foo")
		case OpScan:
			eq(t, string(op.Value.Start), "aaa")
			eq(t, string(op.Value.Limit), "bbb")
		case OpPut:
			if !sawPut {
				eq(t, string(op.Value.Key), "foo")
				sawPut = true
			} else {
				eq(t, string(op.Value.Key), "pv")
				eq(t, string(op.Value.Version), "456")
			}
		case OpDelete:
			eq(t, string(op.Value.Key), "foo")
		case OpSyncgroup:
			eq(t, op.Value.SgId, interfaces.GroupId(1234))
			eq(t, op.Value.Prefixes, []string{"sga", "sgb"})
		case OpSyncSnapshot:
			eq(t, string(op.Value.Key), "aa")
			eq(t, string(op.Value.Version), "123")
		default:
			t.Fatalf("Unexpected op type in entry: %v", entry)
		}
	}
	eq(t, numEntries, wantNumEntries)
}
