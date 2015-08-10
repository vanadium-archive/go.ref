// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"strconv"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/services/watch"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
)

// MakeResumeMarker converts a sequence number to the resume marker.
func MakeResumeMarker(seq uint64) watch.ResumeMarker {
	return watch.ResumeMarker(logEntryKey(seq))
}

func logEntryKey(seq uint64) string {
	// Note: MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	return join(util.LogPrefix, fmt.Sprintf("%016x", seq))
}

// WatchLogBatch returns a batch of watch log records (a transaction) from
// the given database and the new resume marker at the end of the batch.
func WatchLogBatch(st store.Store, resumeMarker watch.ResumeMarker) ([]*LogEntry, watch.ResumeMarker, error) {
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
	parts := split(resumeMarker)
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
