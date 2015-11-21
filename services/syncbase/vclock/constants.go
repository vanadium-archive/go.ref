// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vclock

import (
	"time"
)

const (
	// The pool.ntp.org project is a big virtual cluster of timeservers providing
	// reliable easy to use NTP service for millions of clients.
	// For more information, see: http://www.pool.ntp.org/en/
	NtpDefaultHost = "pool.ntp.org"
	NtpSampleCount = 15
	// Used by VClockD.doNtpUpdate.
	NtpSkewDeltaThreshold = 2 * time.Second
	// Used by VClockD.doLocalUpdate.
	SystemClockDriftThreshold = time.Second
	// Used by MaybeUpdateFromPeerData.
	PeerSyncSkewThreshold = NtpSkewDeltaThreshold
	RebootSkewThreshold   = time.Minute
	MaxNumHops            = 1
)
