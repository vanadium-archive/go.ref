// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

// TODO(sadovsky): Consider using shorter strings.

// Constants related to storage engine keys.
const (
	AppPrefix      = "$app"
	DatabasePrefix = "$database"
	DbInfoPrefix   = "$dbInfo"
	LogPrefix      = "$log"
	RowPrefix      = "$row"
	ServicePrefix  = "$service"
	SyncPrefix     = "$sync"
	TablePrefix    = "$table"
	VersionPrefix  = "$version"
)

// Constants related to object names.
const (
	// Service object name suffix for Syncbase-to-Syncbase RPCs.
	SyncbaseSuffix = "$internal"
	// Separator for batch info in database names.
	BatchSep = ":"
	// Separator for parts of storage engine keys.
	KeyPartSep = ":"
)
