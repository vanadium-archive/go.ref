// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package common

import (
	"fmt"
	"strconv"
	"strings"

	"v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

// JoinKeyParts builds keys for accessing data in the storage engine.
func JoinKeyParts(parts ...string) string {
	return strings.Join(parts, KeyPartSep)
}

// SplitKeyParts is the inverse of JoinKeyParts. Clients are generally
// encouraged to use SplitNKeyParts.
func SplitKeyParts(key string) []string {
	return strings.Split(key, KeyPartSep)
}

// SplitNKeyParts is to SplitKeyParts as strings.SplitN is to strings.Split.
func SplitNKeyParts(key string, n int) []string {
	return strings.SplitN(key, KeyPartSep, n)
}

// StripFirstKeyPartOrDie strips off the first part of the given key. Typically
// used to strip off the key prefixes defined in constants.go. Panics if the
// input string has fewer than two parts.
func StripFirstKeyPartOrDie(key string) string {
	parts := SplitNKeyParts(key, 2)
	if len(parts) < 2 {
		vlog.Fatalf("StripFirstKeyPartOrDie: invalid key %q", key)
	}
	return parts[1]
}

// FirstKeyPart returns the first part of 'key', typically a key prefix defined
// in constants.go.
func FirstKeyPart(key string) string {
	return SplitNKeyParts(key, 2)[0]
}

// IsRowKey returns true iff 'key' is a storage engine key for a row.
func IsRowKey(key string) bool {
	return FirstKeyPart(key) == RowPrefix
}

// ParseRowKey extracts collection and row parts from the given storage engine
// key for a data row. Returns an error if the given key is not a storage engine
// key for a data row.
func ParseRowKey(key string) (collection string, row string, err error) {
	parts := SplitNKeyParts(key, 3)
	pfx := parts[0]
	if len(parts) < 3 || pfx != RowPrefix {
		return "", "", fmt.Errorf("ParseRowKey: invalid key %q", key)
	}
	return parts[1], parts[2], nil
}

// ParseRowKeyOrDie calls ParseRowKey and panics on error.
func ParseRowKeyOrDie(key string) (collection string, row string) {
	collection, row, err := ParseRowKey(key)
	if err != nil {
		vlog.Fatal(err)
	}
	return collection, row
}

// ScanPrefixArgs returns args for sn.Scan() for the specified prefix.
func ScanPrefixArgs(stKeyPrefix, prefix string) ([]byte, []byte) {
	return ScanRangeArgs(stKeyPrefix, util.PrefixRangeStart(prefix), util.PrefixRangeLimit(prefix))
}

// ScanRangeArgs returns args for sn.Scan() for the specified range.
// If limit is "", all rows with keys >= start are included.
func ScanRangeArgs(stKeyPrefix, start, limit string) ([]byte, []byte) {
	fullStart, fullLimit := JoinKeyParts(stKeyPrefix, start), JoinKeyParts(stKeyPrefix, limit)
	if limit == "" {
		fullLimit = util.PrefixRangeLimit(fullLimit)
	}
	return []byte(fullStart), []byte(fullLimit)
}

type BatchType int

const (
	BatchTypeSn BatchType = iota // snapshot
	BatchTypeTx                  // transaction
)

// JoinBatchInfo encodes batch type and id into a single "info" string.
func JoinBatchInfo(batchType BatchType, batchId uint64) string {
	return strings.Join([]string{strconv.Itoa(int(batchType)), strconv.FormatUint(batchId, 10)}, BatchSep)
}

// SplitBatchInfo is the inverse of JoinBatchInfo.
func SplitBatchInfo(batchInfo string) (BatchType, uint64, error) {
	parts := strings.Split(batchInfo, BatchSep)
	if len(parts) != 2 {
		return BatchTypeSn, 0, verror.New(verror.ErrBadArg, nil, batchInfo)
	}
	batchTypeInt, err := strconv.Atoi(parts[0])
	if err != nil {
		return BatchTypeSn, 0, err
	}
	batchType := BatchType(batchTypeInt)
	if batchType != BatchTypeSn && batchType != BatchTypeTx {
		return BatchTypeSn, 0, verror.New(verror.ErrBadArg, nil, batchInfo)
	}
	batchId, err := strconv.ParseUint(parts[1], 0, 64)
	if err != nil {
		return BatchTypeSn, 0, err
	}
	return batchType, batchId, nil
}
