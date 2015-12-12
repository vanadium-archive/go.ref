// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import "v.io/x/ref/services/syncbase/server/util"

// Key prefixes for sync-related metadata.
var (
	dagNodePrefix  = util.JoinKeyParts(util.SyncPrefix, "a")
	dagHeadPrefix  = util.JoinKeyParts(util.SyncPrefix, "b")
	dagBatchPrefix = util.JoinKeyParts(util.SyncPrefix, "c")
	dbssKey        = util.JoinKeyParts(util.SyncPrefix, "d") // database sync state
	sgIdPrefix     = util.JoinKeyParts(util.SyncPrefix, "i") // syncgroup ID --> syncgroup local state
	logPrefix      = util.JoinKeyParts(util.SyncPrefix, "l") // log state
	sgNamePrefix   = util.JoinKeyParts(util.SyncPrefix, "n") // syncgroup name --> syncgroup ID
	sgDataPrefix   = util.JoinKeyParts(util.SyncPrefix, "s") // syncgroup (ID, version) --> syncgroup synced state
)

const (
	// The sync log contains <logPrefix>:<logDataPrefix> records (for data) and
	// <logPrefix>:<sgoid> records (for syncgroup metadata), where <logDataPrefix>
	// is defined below, and <sgoid> is <sgDataPrefix>:<GroupId>.
	logDataPrefix = "d"

	// Types of discovery service attributes.
	discoveryAttrPeer      = "p"
	discoveryAttrSyncgroup = "sg"
)
