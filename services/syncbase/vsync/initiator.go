// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import "time"

var (
	// peerSyncInterval is the duration between two consecutive
	// peer contacts.  During every peer contact, the initiator
	// obtains any pending updates from that peer.
	peerSyncInterval = 50 * time.Millisecond
)

// contactPeers wakes up every peerSyncInterval to select a peer, and
// get updates from it.
func (s *syncService) contactPeers() {
	ticker := time.NewTicker(peerSyncInterval)
	for {
		select {
		case <-s.closed:
			ticker.Stop()
			s.pending.Done()
			return
		case <-ticker.C:
		}

		// Do work.
	}
}
