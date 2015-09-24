// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"strconv"
	"strings"

	"v.io/v23/syncbase/util"
	"v.io/v23/verror"
)

// JoinKeyParts builds keys for accessing data in the storage engine.
func JoinKeyParts(parts ...string) string {
	return strings.Join(parts, KeyPartSep)
}

// SplitKeyParts is the inverse of JoinKeyParts.
func SplitKeyParts(key string) []string {
	return strings.Split(key, KeyPartSep)
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
