// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"time"
)

// Constants related to storage engine keys.
const (
	AppPrefix      = "$app"
	ClockPrefix    = "$clock"
	DatabasePrefix = "$database"
	DbInfoPrefix   = "$dbInfo"
	LogPrefix      = "$log"
	PermsPrefix    = "$perms"
	RowPrefix      = "$row"
	ServicePrefix  = "$service"
	SyncPrefix     = "$sync"
	TablePrefix    = "$table"
	VersionPrefix  = "$version"

	// Note, these are persisted and therefore must not be modified.
	// Below, they are ordered lexicographically by value.
	// TODO(sadovsky): Changing these prefixes breaks various tests. Tests
	// generally shouldn't depend on the values of these constants.
	/*
		AppPrefix      = "a"
		ClockPrefix    = "c"
		DatabasePrefix = "d"
		DbInfoPrefix   = "i"
		LogPrefix      = "l"
		PermsPrefix    = "p"
		RowPrefix      = "r"
		ServicePrefix  = "s"
		TablePrefix    = "t"
		VersionPrefix  = "v"
		SyncPrefix     = "y"
	*/

	// Separator for parts of storage engine keys.
	// TODO(sadovsky): Allow ":" in names and use a different separator here,
	// perhaps "%%".
	KeyPartSep = ":"

	// PrefixRangeLimitSuffix is a key suffix that indicates the end of a prefix
	// range. Must be greater than any character allowed in client-provided keys.
	PrefixRangeLimitSuffix = "\xff"
)

// Constants related to object names.
const (
	// Object name component for Syncbase-to-Syncbase (sync) RPCs.
	// Sync object names have the form:
	//     <syncbase>/%%sync/...
	// FIXME: update all apps, again...
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
)
