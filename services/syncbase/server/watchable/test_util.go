// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"io/ioutil"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/clock"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/leveldb"
	"v.io/syncbase/x/ref/services/syncbase/store/memstore"
	"v.io/v23/vom"
)

// This file provides utility methods for tests related to watchable store.

///////  Functions related to creation/cleanup of store instances  ///////

// createStore returns a store along with a function to destroy the store
// once it is no longer needed.
func createStore(useMemstore bool) (store.Store, func()) {
	var st store.Store
	if useMemstore {
		st = memstore.New()
		return st, func() {
			st.Close()
		}
	} else {
		st = createLevelDB(getPath())
		return st, func() {
			destroyLevelDB(st, getPath())
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
	st, err := leveldb.Open(path)
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

///////  Functions related to watchable store  ///////

func getSeq(st Store) uint64 {
	wst := st.(*wstore)
	return wst.seq
}

func setMockSystemClock(st Store, mockClock clock.SystemClock) {
	wst := st.(*wstore)
	wst.clock.SetSystemClock(mockClock)
}

type LogEntryReader struct {
	stream store.Stream
}

func NewLogEntryReader(st store.Store, seq uint64) *LogEntryReader {
	stream := st.Scan([]byte(getLogEntryKeyPrefix(seq)), []byte(getLogEntryKeyPrefix(seq+1)))
	return &LogEntryReader{stream: stream}
}

func (ler *LogEntryReader) Advance() bool {
	return ler.stream.Advance()
}

func (ler *LogEntryReader) GetEntry() (string, LogEntry) {
	key := string(ler.stream.Key(nil))
	var entry LogEntry = LogEntry{}
	if err := vom.Decode(ler.stream.Value(nil), &entry); err != nil {
		panic(fmt.Errorf("Failed to decode LogEntry for key: %q", key))
	}
	return key, entry
}

///////  Clock related utility code  ///////

// Mock Implementation for SystemClock
type MockSystemClock struct {
	time      time.Time     // current time returned by call to Now()
	increment time.Duration // how much to increment the clock by for subsequent calls to Now()
}

func NewMockSystemClock(firstTimestamp time.Time, increment time.Duration) *MockSystemClock {
	return &MockSystemClock{
		time:      firstTimestamp,
		increment: increment,
	}
}

func (sc *MockSystemClock) Now() time.Time {
	now := sc.time
	sc.time = sc.time.Add(sc.increment)
	return now
}

var _ clock.SystemClock = (*MockSystemClock)(nil)
