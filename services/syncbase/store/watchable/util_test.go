// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"testing"

	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/vclock"
)

// TestGetNextLogSeq tests that the getNextLogSeq helper works on range 0..10
// and continues to work after moving the log start.
func TestGetNextLogSeq(t *testing.T) {
	ist, destroy := createStore()
	defer destroy()
	st, err := Wrap(ist, vclock.NewVClockForTests(nil), &Options{ManagedPrefixes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	// Check 0..10.
	testGetNextLogSeqInterval(t, st, 0, 10)
	// Update log start.
	if _, err := st.watcher.updateLogStartSeq(st, 7); err != nil {
		t.Fatalf("failed to update log start seq: %v", err)
	}
	// Delete some entries before the new start to simulate garbage collection.
	for _, i := range []uint64{1, 3, 4, 5} {
		if err := st.Delete([]byte(logEntryKey(i))); err != nil {
			t.Fatalf("failed to delete log entry %d: %v", i, err)
		}
	}
	// Check 11..20.
	testGetNextLogSeqInterval(t, st, 11, 20)
}

// testGenNextLogSeqInterval tests that the getNextLogSeq helper works on range
// begin..end, assuming that begin is the next seq to be written.
func testGetNextLogSeqInterval(t *testing.T, st store.Store, begin, end uint64) {
	for i := begin; i <= end; i++ {
		seq, err := getNextLogSeq(st)
		if err != nil {
			t.Fatalf("failed to get log seq: %v", err)
		}
		if got, want := seq, i; got != want {
			t.Fatalf("unexpected log seq: got %v, want %v", got, want)
		}
		st.Put([]byte(logEntryKey(i)), nil)
	}
}

// TestPutLogStartSeq tests putting and getting the log start seq.
func TestPutLogStartSeq(t *testing.T) {
	ist, destroy := createStore()
	defer destroy()
	st, err := Wrap(ist, vclock.NewVClockForTests(nil), &Options{ManagedPrefixes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	// Log start should default to 0.
	if gotSeq, err := getLogStartSeq(st); err != nil {
		t.Fatalf("getLogStartSeq() failed: %v", err)
	} else if gotSeq != 0 {
		t.Errorf("expected initial seq 0, got %d", gotSeq)
	}
	// Test log start persistence and encoding.
	for _, seq := range []uint64{1, 42, 1337, 0x1f427, 0x1badb002, 0xdeadbeef} {
		if err := putLogStartSeq(st, seq); err != nil {
			t.Fatalf("putLogStartSeq(%d) failed: %v", seq, err)
		}
		// The log start should be the same when read from the wrapped store or from
		// the watchable store.
		if gotSeq, err := getLogStartSeq(st); err != nil {
			t.Fatalf("getLogStartSeq(st) failed: %v", err)
		} else if gotSeq != seq {
			t.Errorf("expected seq %d, got %d", seq, gotSeq)
		}
		if gotSeq, err := getLogStartSeq(ist); err != nil {
			t.Fatalf("getLogStartSeq(ist) failed: %v", err)
		} else if gotSeq != seq {
			t.Errorf("expected seq %d, got %d", seq, gotSeq)
		}
	}
}
