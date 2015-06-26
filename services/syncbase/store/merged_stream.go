// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package store

import (
	"sort"
)

//////////////////////////////////////////////////////////////
// mergedStream implementation of Stream
//
// This implementation of Stream must take into account writes
// which have occurred since the snapshot was taken on the
// transaction.
//
// The MergeWritesWithStream() function requires uncommitted
// changes to be passed in as an array of WriteOp.

// Create a new stream which merges a snapshot stream with an array of write operations.
func MergeWritesWithStream(sn Snapshot, w []WriteOp, start, limit []byte) Stream {
	// Collect writes with the range specified, then sort them.
	// Note: Writes could contain more than one write for a given key.
	//       The last write is the current state.
	writesMap := map[string]WriteOp{}
	for _, write := range w {
		if string(write.Key) >= string(start) && (string(limit) == "" || string(write.Key) < string(limit)) {
			writesMap[string(write.Key)] = write
		}
	}
	var writesArray WriteOpArray
	for _, writeOp := range writesMap {
		writesArray = append(writesArray, writeOp)
	}
	sort.Sort(writesArray)
	return &mergedStream{
		snapshotStream:      sn.Scan(start, limit),
		writesArray:         writesArray,
		writesCursor:        0,
		unusedSnapshotValue: false,
		snapshotStreamEOF:   false,
		hasValue:            false,
	}
}

type mergedStream struct {
	snapshotStream      Stream
	writesArray         []WriteOp
	writesCursor        int
	unusedSnapshotValue bool
	snapshotStreamEOF   bool
	hasValue            bool // if true, Key() and Value() can be called
	key                 []byte
	value               []byte
}

// Convenience function to check EOF on writesArray
func (s *mergedStream) writesArrayEOF() bool {
	return s.writesCursor >= len(s.writesArray)
}

// If a kv from the snapshot isn't on deck, call
// Advance on the snapshot and set unusedSnapshotValue.
// If EOF encountered, set snapshotStreamEOF.
// If error encountered, return it.
func (s *mergedStream) stageSnapshotKeyValue() error {
	if !s.snapshotStreamEOF && !s.unusedSnapshotValue {
		if !s.snapshotStream.Advance() {
			s.snapshotStreamEOF = true
			if err := s.snapshotStream.Err(); err != nil {
				return err
			}
		}
		s.unusedSnapshotValue = true
	}
	return nil
}

// Pick a kv from either the snapshot or the uncommited writes array.
// If an uncommited write is picked advance past it and return false (also, advance the snapshot
// stream if its current key is equal to the ucommitted delete).
func (s *mergedStream) pickKeyValue() bool {
	if !s.snapshotStreamEOF && (s.writesArrayEOF() || string(s.writesArray[s.writesCursor].Key) > string(s.snapshotStream.Key(nil))) {
		s.key = s.snapshotStream.Key(s.key)
		s.value = s.snapshotStream.Value(s.value)
		s.unusedSnapshotValue = false
		return true
	}
	if !s.snapshotStreamEOF && string(s.writesArray[s.writesCursor].Key) == string(s.snapshotStream.Key(nil)) {
		s.unusedSnapshotValue = false
	}
	if s.writesArrayEOF() || s.writesArray[s.writesCursor].T == DeleteOp {
		s.writesCursor++
		return false
	}
	s.key = CopyBytes(s.key, s.writesArray[s.writesCursor].Key)
	s.value = CopyBytes(s.value, s.writesArray[s.writesCursor].Value)
	s.writesCursor++
	return true
}

func (s *mergedStream) Advance() bool {
	s.hasValue = false
	for true {
		if err := s.stageSnapshotKeyValue(); err != nil {
			return false
		}
		if s.snapshotStreamEOF && s.writesArrayEOF() {
			return false
		}
		if s.pickKeyValue() {
			s.hasValue = true
			return true
		}
	}
	return false // compiler insists on this line
}

// Key implements the Stream interface.
func (s *mergedStream) Key(keybuf []byte) []byte {
	if !s.hasValue {
		panic("nothing staged")
	}
	return CopyBytes(keybuf, s.key)
}

// Value implements the Stream interface.
func (s *mergedStream) Value(valbuf []byte) []byte {
	if !s.hasValue {
		panic("nothing staged")
	}
	return CopyBytes(valbuf, s.value)
}

// Err implements the Stream interface.
func (s *mergedStream) Err() error {
	return s.snapshotStream.Err()
}

// Cancel implements the Stream interface.
func (s *mergedStream) Cancel() {
	s.snapshotStream.Cancel()
	s.hasValue = false
	s.snapshotStreamEOF = true
	s.writesCursor = len(s.writesArray)
}
