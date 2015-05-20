// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"strings"

	"v.io/syncbase/v23/syncbase/util"
)

// JoinKeyParts builds keys for accessing data in the storage engine.
func JoinKeyParts(parts ...string) string {
	// TODO(sadovsky): Figure out which delimeter makes the most sense.
	return strings.Join(parts, ":")
}

// SplitKeyParts is the inverse of JoinKeyParts.
func SplitKeyParts(key string) []string {
	return strings.Split(key, ":")
}

// ScanPrefixArgs returns args for sn.Scan() for the specified prefix.
func ScanPrefixArgs(stKeyPrefix, prefix string) ([]byte, []byte) {
	return ScanRangeArgs(stKeyPrefix, util.PrefixRangeStart(prefix), util.PrefixRangeEnd(prefix))
}

// ScanRangeArgs returns args for sn.Scan() for the specified range.
// If end is "", all rows with keys >= start are included.
func ScanRangeArgs(stKeyPrefix, start, end string) ([]byte, []byte) {
	fullStart, fullEnd := JoinKeyParts(stKeyPrefix, start), JoinKeyParts(stKeyPrefix, end)
	if end == "" {
		fullEnd = util.PrefixRangeEnd(fullEnd)
	}
	return []byte(fullStart), []byte(fullEnd)
}
