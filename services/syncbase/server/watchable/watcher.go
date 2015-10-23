// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"strconv"
	"sync"

	"v.io/v23/services/watch"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// watcher maintains a state and a condition variable. The watcher sends
// a broadcast signal every time the state changes. The state is increased
// by 1 every time the store has new data. Initially the state equals to 1.
// If the state becomes 0, then the watcher is closed and the state will not
// be changed later.
// TODO(rogulenko): Broadcast a signal from time to time to unblock waiting
// clients.
type watcher struct {
	mu    *sync.RWMutex
	cond  *sync.Cond
	state uint64
}

func newWatcher() *watcher {
	mu := &sync.RWMutex{}
	return &watcher{
		mu:    mu,
		cond:  sync.NewCond(mu.RLocker()),
		state: 1,
	}
}

// close closes the watcher.
func (w *watcher) close() {
	w.mu.Lock()
	w.state = 0
	w.cond.Broadcast()
	w.mu.Unlock()
}

// broadcastUpdates broadcast the update notification to watch clients.
func (w *watcher) broadcastUpdates() {
	w.mu.Lock()
	if w.state != 0 {
		w.state++
		w.cond.Broadcast()
	} else {
		vlog.Error("broadcastUpdates() called on a closed watcher")
	}
	w.mu.Unlock()
}

// WatchUpdates returns a function that can be used to watch for changes of
// the database. The store maintains a state (initially 1) that is increased
// by 1 every time the store has new data. The waitForChange function takes
// the last returned state and blocks until the state changes, returning the new
// state. State equal to 0 means the store is closed and no updates will come
// later. If waitForChange function takes a state different from the current
// state of the store or the store is closed, the waitForChange function returns
// immediately. It might happen that the waitForChange function returns
// a non-zero state equal to the state passed as the argument. This behavior
// helps to unblock clients if the store doesn't have updates for a long period
// of time.
func WatchUpdates(st store.Store) (waitForChange func(state uint64) uint64) {
	// TODO(rogulenko): Remove dynamic type assertion here and in other places.
	watcher := st.(*wstore).watcher
	return func(state uint64) uint64 {
		watcher.cond.L.Lock()
		defer watcher.cond.L.Unlock()
		if watcher.state != 0 && watcher.state == state {
			watcher.cond.Wait()
		}
		return watcher.state
	}
}

// GetResumeMarker returns the ResumeMarker that points to the current end
// of the event log.
func GetResumeMarker(st store.StoreReader) (watch.ResumeMarker, error) {
	seq, err := getNextLogSeq(st)
	return watch.ResumeMarker(logEntryKey(seq)), err
}

// MakeResumeMarker converts a sequence number to the resume marker.
func MakeResumeMarker(seq uint64) watch.ResumeMarker {
	return watch.ResumeMarker(logEntryKey(seq))
}

func logEntryKey(seq uint64) string {
	// Note: MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	return join(util.LogPrefix, fmt.Sprintf("%016x", seq))
}

// ReadBatchFromLog returns a batch of watch log records (a transaction) from
// the given database and the new resume marker at the end of the batch.
func ReadBatchFromLog(st store.Store, resumeMarker watch.ResumeMarker) ([]*LogEntry, watch.ResumeMarker, error) {
	seq, err := parseResumeMarker(string(resumeMarker))
	if err != nil {
		return nil, resumeMarker, err
	}
	_, scanLimit := util.ScanPrefixArgs(util.LogPrefix, "")
	scanStart := resumeMarker
	endOfBatch := false

	// Use the store directly to scan these read-only log entries, no need
	// to create a snapshot since they are never overwritten.  Read and
	// buffer a batch before processing it.
	var logs []*LogEntry
	stream := st.Scan(scanStart, scanLimit)
	for stream.Advance() {
		seq++
		var logEnt LogEntry
		if err := vom.Decode(stream.Value(nil), &logEnt); err != nil {
			return nil, resumeMarker, err
		}

		logs = append(logs, &logEnt)

		// Stop if this is the end of the batch.
		if logEnt.Continued == false {
			endOfBatch = true
			break
		}
	}

	if err = stream.Err(); err != nil {
		return nil, resumeMarker, err
	}
	if !endOfBatch {
		if len(logs) > 0 {
			vlog.Fatalf("end of batch not found after %d entries", len(logs))
		}
		return nil, resumeMarker, nil
	}
	return logs, watch.ResumeMarker(logEntryKey(seq)), nil
}

func parseResumeMarker(resumeMarker string) (uint64, error) {
	parts := util.SplitNKeyParts(resumeMarker, 2)
	if len(parts) != 2 {
		return 0, verror.New(watch.ErrUnknownResumeMarker, nil, resumeMarker)
	}
	seq, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return 0, verror.New(watch.ErrUnknownResumeMarker, nil, resumeMarker)
	}
	return seq, nil
}

// logEntryExists returns true iff the log contains an entry with the given
// sequence number.
func logEntryExists(st store.StoreReader, seq uint64) (bool, error) {
	_, err := st.Get([]byte(logEntryKey(seq)), nil)
	if err != nil && verror.ErrorID(err) != store.ErrUnknownKey.ID {
		return false, err
	}
	return err == nil, nil
}

// getNextLogSeq returns the next sequence number to be used for a new commit.
// NOTE: this function assumes that all sequence numbers in the log represent
// some range [start, limit] without gaps.
func getNextLogSeq(st store.StoreReader) (uint64, error) {
	// Determine initial value for seq.
	// TODO(sadovsky): Consider using a bigger seq.

	// Find the beginning of the log.
	it := st.Scan(util.ScanPrefixArgs(util.LogPrefix, ""))
	if !it.Advance() {
		return 0, nil
	}
	if it.Err() != nil {
		return 0, it.Err()
	}
	seq, err := parseResumeMarker(string(it.Key(nil)))
	if err != nil {
		return 0, err
	}
	var step uint64 = 1
	// Suppose the actual value we are looking for is S. First, we estimate the
	// range for S. We find seq, step: seq < S <= seq + step.
	for {
		if ok, err := logEntryExists(st, seq+step); err != nil {
			return 0, err
		} else if !ok {
			break
		}
		seq += step
		step *= 2
	}
	// Next we keep the seq < S <= seq + step invariant, reducing step to 1.
	for step > 1 {
		step /= 2
		if ok, err := logEntryExists(st, seq+step); err != nil {
			return 0, err
		} else if ok {
			seq += step
		}
	}
	// Now seq < S <= seq + 1, thus S = seq + 1.
	return seq + 1, nil
}
