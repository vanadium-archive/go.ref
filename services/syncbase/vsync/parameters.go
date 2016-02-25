// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import "time"

var (
	// peerSyncInterval is the duration between two consecutive peer
	// contacts. During every peer contact, the initiator obtains any
	// pending updates from that peer.
	peerSyncInterval = 50 * time.Millisecond

	// channelTimeout is the duration at which health checks will be requested on
	// connections to peers.
	channelTimeout = 2 * time.Second

	// peerConnectionTimeout is the time duration we wait for a connection to be
	// established with a peer.
	peerConnectionTimeout = 2 * time.Second

	// syncConnectionTimeout is the duration we wait for syncing to begin with a
	// peer. A value of 0 means we will only use connections that exist in our
	// cache.
	//
	// TODO(hpucha): Make sync connection timeout dynamic based on ping latency.
	// E.g. perhaps we should use a 2s ping timeout, and use k*pingLatency (for
	// some k) as getDeltas timeout
	syncConnectionTimeout = 0

	// memberViewTTL is the shelf-life of the aggregate view of syncgroup members.
	memberViewTTL = 2 * time.Second

	// pingFailuresCap is the number of failures to cap off at when
	// computing how many peer manager rounds to wait before retrying to
	// ping peers via the mount table. This implies that a peer manager will
	// backoff a maximum of (2^pingFailuresCap) rounds or
	// (2^pingFailuresCap)*peerManagementInterval in terms of duration.
	pingFailuresCap uint64 = 6

	// pingFanout is the maximum number of peers that can be pinged in
	// parallel.  TODO(hpucha): support ping fanout of 0.
	pingFanout = 10

	// peerManagementInterval is the duration between two rounds of peer
	// management actions.
	peerManagementInterval = peerSyncInterval / 5

	// watchPollInterval is the duration between consecutive watch polling
	// events across all app databases.  Every watch event loops across all
	// app databases and fetches from each one at most one batch update
	// (transaction) to process.
	// TODO(rdaoud): add a channel between store and watch to get change
	// notifications instead of using a polling solution.
	watchPollInterval = 100 * time.Millisecond
)
