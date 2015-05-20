// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

// TODO(sadovsky): Consider using shorter strings.
const (
	ServicePrefix  = "$service"
	AppPrefix      = "$app"
	DbInfoPrefix   = "$dbInfo"
	DatabasePrefix = "$database"
	TablePrefix    = "$table"
	RowPrefix      = "$row"
	SyncPrefix     = "$sync"
)

const (
	// Service object name suffix for Syncbase internal communication.
	SyncbaseSuffix = "$internal"
)
