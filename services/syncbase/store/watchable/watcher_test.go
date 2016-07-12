// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"v.io/v23/services/watch"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/vclock"
)

// TestWatchLogBatch tests fetching a batch of log records.
func TestWatchLogBatch(t *testing.T) {
	runTest(t, []string{common.RowPrefix, common.CollectionPermsPrefix}, runWatchLogBatchTest)
}

// runWatchLogBatchTest tests fetching a batch of log records.
func runWatchLogBatchTest(t *testing.T, st store.Store) {
	// Create a set of batches to fill the log queue.
	numTx, numPut := 3, 4

	makeKeyVal := func(batchNum, recNum int) ([]byte, []byte) {
		key := common.JoinKeyParts(common.RowPrefix, fmt.Sprintf("foo-%d-%d", batchNum, recNum))
		val := fmt.Sprintf("val-%d-%d", batchNum, recNum)
		return []byte(key), []byte(val)
	}

	for i := 0; i < numTx; i++ {
		tx := st.NewTransaction()
		for j := 0; j < numPut; j++ {
			key, val := makeKeyVal(i, j)
			if err := tx.Put(key, val); err != nil {
				t.Errorf("cannot put %s (%s): %v", key, val, err)
			}
		}
		tx.Commit()
	}

	// Fetch the batches and a few more empty fetches and verify them.
	var seq uint64 = 0
	var wantSeq uint64 = 0

	for i := 0; i < (numTx + 3); i++ {
		logs, newSeq, err := readBatchFromLog(st, seq)
		if err != nil {
			t.Fatalf("can't get watch log batch: %v", err)
		}
		if i < numTx {
			if len(logs) != numPut {
				t.Errorf("log fetch (i=%d) wrong log seq: %d instead of %d",
					i, len(logs), numPut)
			}

			wantSeq += uint64(len(logs))
			if newSeq != wantSeq {
				t.Errorf("log fetch (i=%d) wrong seq: %d instead of %d",
					i, newSeq, wantSeq)
			}

			for j, log := range logs {
				var op PutOp
				if err := log.Op.ToValue(&op); err != nil {
					t.Fatalf("ToValue failed: %v", err)
				}
				expKey, expVal := makeKeyVal(i, j)
				key := op.Key
				if !bytes.Equal(key, expKey) {
					t.Errorf("log fetch (i=%d, j=%d) bad key: %s instead of %s",
						i, j, key, expKey)
				}
				tx := st.NewTransaction()
				var val []byte
				val, err := GetAtVersion(nil, tx, key, val, op.Version)
				if err != nil {
					t.Errorf("log fetch (i=%d, j=%d) cannot GetAtVersion(): %v", i, j, err)
				}
				if !bytes.Equal(val, expVal) {
					t.Errorf("log fetch (i=%d, j=%d) bad value: %s instead of %s",
						i, j, val, expVal)
				}
				tx.Abort()
			}
		} else {
			if logs != nil || newSeq != seq {
				t.Errorf("NOP log fetch (i=%d) had changes: %d logs, seq %d",
					i, len(logs), newSeq)
			}
		}
		seq = newSeq
	}
}

// TestBroadcastUpdates tests broadcasting updates to watch clients.
func TestBroadcastUpdates(t *testing.T) {
	w := newWatcher(0)

	// Update broadcast should never block. It should be safe to call with no
	// clients registered.
	w.broadcastUpdates()
	w.broadcastUpdates()

	// Never-receiving client should not block watcher.
	_, cancel1 := w.watchUpdates(0)
	defer cancel1()

	// Cancelled client should not affect watcher.
	c2, cancel2 := w.watchUpdates(0)
	cancel2()
	// Cancel should be idempotent.
	cancel2()

	// Channel should be closed when client is cancelled.
	select {
	case _, ok := <-c2.Wait():
		if ok {
			t.Fatalf("cancel2 was called, c2 channel should be drained and closed")
		}
	default:
		t.Fatalf("cancel2 was called, c2 channel should be closed")
	}
	if verror.ErrorID(c2.Err()) != verror.ErrCanceled.ID {
		t.Fatalf("expected c2.Err() to return ErrCanceled, got: %v", c2.Err())
	}

	// Update broadcast should not block client registration or vice versa.
	timer1 := time.After(10 * time.Second)
	registerLoop1 := make(chan bool)
	go func() {
		for i := 0; i < 5000; i++ {
			_, canceli := w.watchUpdates(0)
			defer canceli()
		}
		registerLoop1 <- true
	}()

	c3, cancel3 := w.watchUpdates(0)

	for i := 0; i < 5000; i++ {
		w.broadcastUpdates()
	}

	select {
	case <-registerLoop1:
		// ok
	case <-timer1:
		t.Fatalf("registerLoop1 didn't finish after 10s")
	}

	// Wait for broadcast to fully propagate out of watcherLoop.
	time.Sleep(1 * time.Second)

	// chan3 should have a single pending notification.
	select {
	case _, ok := <-c3.Wait():
		if !ok {
			t.Fatalf("c3 channel should not be closed")
		}
	default:
		t.Fatalf("c3 channel should have a notification")
	}
	select {
	case <-c3.Wait():
		t.Fatalf("c3 channel should not have another notification")
	default:
		// ok
	}
	if c3.Err() != nil {
		t.Fatalf("expected c3.Err() to return nil, got: %v", c3.Err())
	}

	// After notification was read, chan3 still receives updates.
	go func() {
		time.Sleep(500 * time.Millisecond)
		w.broadcastUpdates()
	}()

	select {
	case _, ok := <-c3.Wait():
		if !ok {
			t.Fatalf("c3 channel should not be closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("c3 channel didn't receive after 5s")
	}
	if c3.Err() != nil {
		t.Fatalf("expected c3.Err() to return nil, got: %v", c3.Err())
	}

	// Closing the watcher.
	w.close()
	// Close should be idempotent.
	w.close()

	// Client channels should be closed when watcher is closed.
	select {
	case _, ok := <-c3.Wait():
		if ok {
			t.Fatalf("watcher was closed, c3 channel should be drained and closed")
		}
	default:
		t.Fatalf("watcher was closed, c3 channel should be closed")
	}
	if verror.ErrorID(c3.Err()) != verror.ErrAborted.ID {
		t.Fatalf("expected c3.Err() to return ErrAborted, got: %v", c3.Err())
	}

	// Cancel is safe to call after the store is closed.
	cancel3()
	// ErrAborted should be preserved instead of being overridden by ErrCanceled.
	if verror.ErrorID(c3.Err()) != verror.ErrAborted.ID {
		t.Fatalf("expected c3.Err() to return ErrAborted, got: %v", c3.Err())
	}

	// watchUpdates is safe to call after the store is closed, returning closed
	// channel.
	c4, cancel4 := w.watchUpdates(0)

	select {
	case _, ok := <-c4.Wait():
		if ok {
			t.Fatalf("watcher was closed, c4 channel should be drained and closed")
		}
	default:
		t.Fatalf("watcher was closed, c4 channel should be closed")
	}
	if verror.ErrorID(c4.Err()) != verror.ErrAborted.ID {
		t.Fatalf("expected c4.Err() to return ErrAborted, got: %v", c4.Err())
	}

	cancel4()

	// broadcastUpdates is safe to call after the store is closed, although it
	// logs an error message.
	w.broadcastUpdates()
}

// TestUpdateLogStart tests that UpdateLogStart moves the log start up to the
// earliest active watcher's seq, including syncSeq, and that NextBatchFromLog
// correctly iterates over the log.
func TestUpdateLogStart(t *testing.T) {
	ist, destroy := createStore()
	defer destroy()
	st, err := Wrap(ist, vclock.NewVClockForTests(nil), &Options{ManagedPrefixes: []string{common.RowPrefix, common.CollectionPermsPrefix}})
	if err != nil {
		t.Fatal(err)
	}
	// UpdateLogStart(0) should work on an empty log.
	updateLogStart(t, st, 0, 0)
	putBatch(t, st, 0, 3)
	putBatch(t, st, 3, 1)
	// UpdateLogStart(3) with no active watchers should move the log start to 3.
	updateLogStart(t, st, 3, 3)
	putBatch(t, st, 4, 5)
	// UpdateLogStart should be idempotent if called with the same value.
	updateLogStart(t, st, 3, 3)
	// Start watchers @3 and @9.
	w1, w1cancel := st.WatchUpdates(MakeResumeMarker(3))
	w2, _ := st.WatchUpdates(MakeResumeMarker(9))
	// Watching from 0 should fail because log start has moved to 3.
	if wFail, _ := st.WatchUpdates(MakeResumeMarker(0)); verror.ErrorID(wFail.Err()) != watch.ErrUnknownResumeMarker.ID {
		t.Fatalf("WatchUpdates with old resume marker should have failed with ErrUnknownResumeMarker, got: %v", err)
	}
	putBatch(t, st, 9, 1)
	// UpdateLogStart(9): sync @9, w1 @3, w2 @9 => log start stays at 3.
	updateLogStart(t, st, 9, 3)
	// w1 still @3, next @4.
	expectBatch(t, st, w1, 3, 1)
	// UpdateLogStart(9): sync @9, w1 @3, w2 @9 => log start stays at 3.
	updateLogStart(t, st, 9, 3)
	// w1 @4, next @9.
	expectBatch(t, st, w1, 4, 5)
	// UpdateLogStart(9): sync @9, w1 @4, w2 @9 => log start moves to 4.
	updateLogStart(t, st, 9, 4)
	// UpdateLogStart requires the sync resmark to monotonically increase.
	if _, err := st.UpdateLogStart(MakeResumeMarker(3)); err == nil {
		t.Fatalf("UpdateLogStart should fail when sync resume marker is smaller than log start")
	}
	// Above UpdateLogStart with an invalid sync resmark should have had no effect.
	updateLogStart(t, st, 9, 4)
	putBatch(t, st, 10, 2)
	// w1 @9, next @10.
	expectBatch(t, st, w1, 9, 1)
	// w1 @10, next @12.
	expectBatch(t, st, w1, 10, 2)
	// UpdateLogStart(9): sync @9, w1 @10, w2 @9 => log start stays at 9.
	updateLogStart(t, st, 9, 9)
	// UpdateLogStart(10): sync @10, w1 @10, w2 @9 => log start stays at 9.
	updateLogStart(t, st, 10, 9)
	// w2 still @9, next @10.
	expectBatch(t, st, w2, 9, 1)
	// w2 @10, next @12.
	expectBatch(t, st, w2, 10, 2)
	// w2 @12, nothing to read, next @12.
	expectBatch(t, st, w2, 12, 0)
	// UpdateLogStart(12): sync @12, w1 @10, w2 @12 => log start moves to 10.
	updateLogStart(t, st, 12, 10)
	putBatch(t, st, 12, 5)
	// w2 still @12, next @17.
	expectBatch(t, st, w2, 12, 5)
	// w2 @17, nothing to read, next @17.
	expectBatch(t, st, w2, 17, 0)
	putBatch(t, st, 17, 1)
	// UpdateLogStart(12): sync @12, w1 @10, w2 @17 => log start stays at 10.
	updateLogStart(t, st, 12, 10)
	// Stop w1.
	w1cancel()
	if _, _, err := w1.NextBatchFromLog(st); verror.ErrorID(err) != verror.ErrCanceled.ID {
		t.Fatalf("NextBatchFromLog after watcher cancel should have failed with ErrCanceled, got: %v", err)
	}
	// UpdateLogStart(12): sync @12, w2 @17 => log start moves to 12.
	updateLogStart(t, st, 12, 12)
	// UpdateLogStart(18): sync @18, w2 @17 => log start moves to 17.
	updateLogStart(t, st, 18, 17)
	// Simulate store close and reopen by closing the watcher (which stops w2) and
	// rewrapping ist.
	st.watcher.close()
	if _, _, err := w2.NextBatchFromLog(st); verror.ErrorID(err) != verror.ErrAborted.ID {
		t.Fatalf("NextBatchFromLog after watcher closure should have failed with ErrAborted, got: %v", err)
	}
	if _, err := st.UpdateLogStart(MakeResumeMarker(18)); verror.ErrorID(err) != verror.ErrAborted.ID {
		t.Fatalf("UpdateLogStart on closed watcher should have failed with ErrAborted, got: %v", err)
	}
	st, err = Wrap(ist, vclock.NewVClockForTests(nil), &Options{ManagedPrefixes: []string{common.RowPrefix, common.CollectionPermsPrefix}})
	if err != nil {
		t.Fatal(err)
	}
	// Before starting watchers, test sync resume marker sanity check.
	if _, err := st.UpdateLogStart(MakeResumeMarker(42)); err == nil {
		t.Fatalf("UpdateLogStart should fail when sync resume marker is greater than seq")
	}
	putBatch(t, st, 18, 2)
	putBatch(t, st, 20, 5)
	putBatch(t, st, 25, 2)
	// UpdateLogStart(20) with no active watchers should move the log start to 20.
	updateLogStart(t, st, 20, 20)
	// Watching from 18 should fail because log start has moved to 20.
	if wFail, _ := st.WatchUpdates(MakeResumeMarker(18)); verror.ErrorID(wFail.Err()) != watch.ErrUnknownResumeMarker.ID {
		t.Fatalf("WatchUpdates with old resume marker should have failed with ErrUnknownResumeMarker, got: %v", err)
	}
	// Start watcher @20.
	w3, w3cancel := st.WatchUpdates(MakeResumeMarker(20))
	// Watching from 42 should fail because log end hasn't reached 42 yet.
	if wFail, _ := st.WatchUpdates(MakeResumeMarker(42)); verror.ErrorID(wFail.Err()) != watch.ErrUnknownResumeMarker.ID {
		t.Fatalf("WatchUpdates with resume marker greater than seq should have failed with ErrUnknownResumeMarker, got: %v", err)
	}
	// w3 still @20, next @25.
	expectBatch(t, st, w3, 20, 5)
	// UpdateLogStart(27): sync @27, w3 @20 => log start stays at 20.
	updateLogStart(t, st, 27, 20)
	// w3 @25, next @27.
	expectBatch(t, st, w3, 25, 2)
	// UpdateLogStart(27): sync @27, w3 @25 => log start moves to 25.
	updateLogStart(t, st, 27, 25)
	// Stop w3.
	w3cancel()
	// UpdateLogStart(27) with no active watchers should move the log start to 27.
	updateLogStart(t, st, 27, 27)
}

// putBatch puts a batch with size changes, asserting that it starts at wantSeq.
func putBatch(t *testing.T, st *Store, wantSeq, size uint64) {
	seq := st.getSeq()
	if seq != wantSeq {
		t.Errorf("putBatch %d: unexpected seq before: %d, want %d", wantSeq, seq, wantSeq)
	}
	if err := RunInTransaction(st, func(tx *Transaction) error {
		for i := uint64(0); i < size; i++ {
			if err := tx.Put([]byte(dummyRowKey(seq+i)), []byte("value")); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("putBatch %d: RunInTransaction failed: %v", wantSeq, err)
	}
	if got, want := st.getSeq(), seq+size; got != want {
		t.Fatalf("putBatch %d: unexpected seq after: %d, want %d", wantSeq, got, want)
	}
}

// expectBatch reads the next batch for the watcher using NextBatchFromLog,
// asserting that it starts at wantSeq and has wantSize changes.
func expectBatch(t *testing.T, st *Store, watcher *Client, wantSeq, wantSize uint64) {
	gotLogs, gotRm, err := watcher.NextBatchFromLog(st)
	if err != nil {
		t.Fatalf("expectBatch %d: NextBatchFromLog failed: %v", wantSeq, err)
	}
	if got, want := uint64(len(gotLogs)), wantSize; got != want {
		t.Errorf("expectBatch %d: expected %d logs, got %d", wantSeq, want, got)
	}
	if len(gotLogs) > 0 {
		if endSeq, err := parseResumeMarker(string(gotRm)); err != nil {
			t.Fatalf("expectBatch %d: got invalid resume marker %q: %v", wantSeq, gotRm, err)
		} else if got, want := endSeq-uint64(len(gotLogs)), wantSeq; got != want {
			t.Errorf("expectBatch %d: expected logs starting from seq %d, got %d", wantSeq, want, got)
		}
	}
}

// updateLogStart calls UpdateLogStart with syncSeq, asserting that the updated
// log start, both returned and persisted, matches wantSeq.
func updateLogStart(t *testing.T, st *Store, syncSeq, wantSeq uint64) {
	gotResMark, err := st.UpdateLogStart(MakeResumeMarker(syncSeq))
	if err != nil {
		t.Fatalf("UpdateLogStart %d: failed: %v", syncSeq, err)
	}
	gotSeq, err := parseResumeMarker(string(gotResMark))
	if err != nil {
		t.Fatalf("UpdateLogStart %d: returned invalid resume marker %q: %v", syncSeq, gotResMark, err)
	}
	if gotSeq != wantSeq {
		t.Errorf("UpdateLogStart %d: expected updated seq %d, got %d", syncSeq, wantSeq, gotSeq)
	}
	if readSeq, err := getLogStartSeq(st); err != nil {
		t.Fatalf("UpdateLogStart %d: getLogStartSeq failed: %v", syncSeq, err)
	} else if readSeq != wantSeq {
		t.Errorf("UpdateLogStart %d: expected persisted seq %d, got %d", syncSeq, wantSeq, readSeq)
	}
}

func dummyRowKey(seq uint64) string {
	return common.JoinKeyParts(common.RowPrefix, "u,c", fmt.Sprintf("r%08d", seq))
}
