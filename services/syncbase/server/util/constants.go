// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

// Constants related to storage engine keys.
// Note, these are persisted and therefore must not be modified.
const (
	AppPrefix        = "a"
	VClockPrefix     = "c"
	DatabasePrefix   = "d"
	DbInfoPrefix     = "i"
	LogPrefix        = "l"
	PermsPrefix      = "p"
	RowPrefix        = "r"
	ServicePrefix    = "s"
	TablePrefix      = "t"
	VersionPrefix    = "v"
	PermsIndexPrefix = "x"
	SyncPrefix       = "y"

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
