// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"io/ioutil"
	"math"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/clock"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/leveldb"
	"v.io/syncbase/x/ref/services/syncbase/store/memstore"
	"v.io/v23/vom"
)

// This file provides utility methods for tests related to watchable store.

////////////////////////////////////////////////////////////
// Functions for store creation/cleanup

// createStore returns a store along with a function to destroy the store
// once it is no longer needed.
func createStore() (store.Store, func()) {
	var st store.Store
	// With Memstore, TestReadWriteRandom is slow with ManagedPrefixes=nil since
	// every watchable.Store.Get() takes a snapshot, and memstore snapshots are
	// relatively expensive since the entire data map is copied. LevelDB snapshots
	// are cheap, so with LevelDB ManagedPrefixes=nil is still reasonably fast.
	if false {
		st = memstore.New()
		return st, func() {
			st.Close()
		}
	} else {
		path := getPath()
		st = createLevelDB(path)
		return st, func() {
			destroyLevelDB(st, path)
		}
	}
}

func getPath() string {
	path, err := ioutil.TempDir("", "syncbase_leveldb")
	if err != nil {
		panic(fmt.Sprintf("can't create temp dir: %v", err))
	}
	return path
}

func createLevelDB(path string) store.Store {
	st, err := leveldb.Open(path, leveldb.OpenOptions{CreateIfMissing: true, ErrorIfExists: true})
	if err != nil {
		panic(fmt.Sprintf("can't open db at %v: %v", path, err))
	}
	return st
}

func destroyLevelDB(st store.Store, path string) {
	st.Close()
	if err := leveldb.Destroy(path); err != nil {
		panic(fmt.Sprintf("can't destroy db at %v: %v", path, err))
	}
}

////////////////////////////////////////////////////////////
// Functions related to watchable store

func getSeq(st Store) uint64 {
	wst := st.(*wstore)
	return wst.seq
}

// logEntryReader provides a stream-like interface to scan over the log entries
// of a single batch, starting for a given sequence number.  It opens a stream
// that scans the log from the sequence number given.  It stops after reading
// the last entry in that batch (indicated by a false Continued flag).
type logEntryReader struct {
	stream store.Stream // scan stream on the store Database
	done   bool         // true after reading the last batch entry
	key    string       // key of most recent log entry read
	entry  LogEntry     // most recent log entry read
}

func newLogEntryReader(st store.Store, seq uint64) *logEntryReader {
	stream := st.Scan([]byte(logEntryKey(seq)), []byte(logEntryKey(math.MaxUint64)))
	return &logEntryReader{stream: stream}
}

func (ler *logEntryReader) Advance() bool {
	if ler.done {
		return false
	}

	if ler.stream.Advance() {
		ler.key = string(ler.stream.Key(nil))
		if err := vom.Decode(ler.stream.Value(nil), &ler.entry); err != nil {
			panic(fmt.Errorf("Failed to decode LogEntry for key: %q", ler.key))
		}
		if ler.entry.Continued == false {
			ler.done = true
		}
		return true
	}

	ler.key = ""
	ler.entry = LogEntry{}
	return false
}

func (ler *logEntryReader) GetEntry() (string, LogEntry) {
	return ler.key, ler.entry
}

////////////////////////////////////////////////////////////
// Clock related utility code

type mockSystemClock struct {
	time      time.Time     // current time returned by call to Now()
	increment time.Duration // how much to increment the clock by for subsequent calls to Now()
}

func newMockSystemClock(firstTimestamp time.Time, increment time.Duration) *mockSystemClock {
	return &mockSystemClock{
		time:      firstTimestamp,
		increment: increment,
	}
}

func (sc *mockSystemClock) Now() time.Time {
	now := sc.time
	sc.time = sc.time.Add(sc.increment)
	return now
}

func (sc *mockSystemClock) ElapsedTime() (time.Duration, error) {
	return sc.increment, nil
}

var _ clock.SystemClock = (*mockSystemClock)(nil)
