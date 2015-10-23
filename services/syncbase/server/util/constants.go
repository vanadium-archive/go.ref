// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"time"
)

// Constants related to storage engine keys.
// Note, these are persisted and therefore must not be modified.
// TODO(sadovsky): Use one-byte strings. Changing these prefixes breaks various
// tests. Tests generally shouldn't depend on the values of these constants.
const (
	AppPrefix        = "$app"
	ClockPrefix      = "$clock"
	DatabasePrefix   = "$database"
	DbInfoPrefix     = "$dbInfo"
	LogPrefix        = "$log"
	PermsPrefix      = "$perms"
	PermsIndexPrefix = "$iperms"
	RowPrefix        = "$row"
	ServicePrefix    = "$service"
	SyncPrefix       = "$sync"
	TablePrefix      = "$table"
	VersionPrefix    = "$version"

	// KeyPartSep is a separator for parts of storage engine keys, e.g. separating
	// table name from row key.
	KeyPartSep = "\xfe"

	// PrefixRangeLimitSuffix is a key suffix that indicates the end of a prefix
	// range. Must be greater than any character allowed in client-specified keys.
	PrefixRangeLimitSuffix = "\xff"
)

// Constants related to object names.
const (
	// Object name component for Syncbase-to-Syncbase (sync) RPCs.
	// Sync object names have the form:
	//     <syncbase>/%%sync/...
	SyncbaseSuffix = "%%sync"
	// Separator for batch info in database names.
	// Batch object names have the form:
	//     <syncbase>/<app>/<database>%%<batchInfo>/...
	BatchSep = "%%"
)

// Constants related to syncbase clock.
const (
	// The pool.ntp.org project is a big virtual cluster of timeservers,
	// providing reliable, easy-to-use NTP service for millions of clients.
	// See more at http://www.pool.ntp.org/en/
	NtpServerPool            = "pool.ntp.org"
	NtpSampleCount           = 15
	LocalClockDriftThreshold = float64(time.Second)
	NtpDiffThreshold         = float64(2 * time.Second)
	PeerSyncDiffThreshold    = NtpDiffThreshold
	RebootTolerance          = float64(time.Minute)
	HopTolerance             = 2
)
