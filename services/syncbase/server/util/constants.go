// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"time"
)

// TODO(sadovsky): Consider using shorter strings.

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
)

// Constants related to object names.
const (
	// Service object name suffix for Syncbase-to-Syncbase RPCs.
	SyncbaseSuffix = "$sync"
	// Separator for batch info in database names.
	BatchSep = ":"
	// Separator for parts of storage engine keys.
	KeyPartSep = ":"
	// PrefixRangeLimitSuffix is the suffix of a key which indicates the end of
	// a prefix range. Should be more than any regular key in the store.
	// TODO(rogulenko): Change this constant to something out of the UTF8 space.
	PrefixRangeLimitSuffix = "~"
)

// Constants related to syncbase clock.
const (
	// The pool.ntp.org project is a big virtual cluster of timeservers
	// providing reliable easy to use NTP service for millions of clients.
	// See more at http://www.pool.ntp.org/en/
	NtpServerPool            = "pool.ntp.org"
	NtpSampleCount           = 15
	LocalClockDriftThreshold = float64(time.Second)
	NtpDiffThreshold         = float64(2 * time.Second)
)
