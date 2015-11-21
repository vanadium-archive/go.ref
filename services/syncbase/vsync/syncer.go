// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

// Policies to pick a peer to sync with.
const (
	// Picks a peer at random from the available set.
	selectRandom = iota

	// Picks a peer based on network availability and available Syncbases
	// over the neighborhood via discovery.
	selectNeighborhoodAware

	// TODO(hpucha): implement these policies.
	// Picks a peer with most differing generations.
	selectMostDiff

	// Picks a peer that was synced with the furthest in the past.
	selectOldest
)

var (
	// peerSyncInterval is the duration between two consecutive peer
	// contacts. During every peer contact, the initiator obtains any
	// pending updates from that peer.
	peerSyncInterval = 50 * time.Millisecond

	// We wait for connectionTimeOut duration for a connection to be
	// established with a peer reachable via a chosen mount table.
	connectionTimeOut = 2 * time.Second

	// Number of failures to cap off at when computing how many sync rounds
	// to wait before retrying to contact a peer via the mount table.
	maxFailuresForBackoff uint64 = 6
)

// connInfo holds the information needed to connect to a peer.
//
// TODO(hpucha): Add hints to decide if both neighborhood and mount table must
// be tried. Currently, if addrs are set, only addrs are tried.
type connInfo struct {
	// Name of the peer relative to the mount table chosen by the syncgroup
	// creator.
	relName string

	// Mount tables via which this peer might be reachable.
	mtTbls []string

	// Network address of the peer if available. For example, this can be
	// obtained from neighborhood discovery.
	addrs []string
}

// peerSelector defines the interface that a peer selection algorithm must
// provide.
type peerSelector interface {
	// pickPeer picks a Syncbase to sync with.
	pickPeer(ctx *context.T) (connInfo, error)

	// updatePeerFromSyncer updates information for a peer that the syncer
	// attempts to connect to.
	updatePeerFromSyncer(ctx *context.T, peer connInfo, attemptTs time.Time, failed bool) error

	// updatePeerFromResponder updates information for a peer that the
	// responder responds to.
	updatePeerFromResponder(ctx *context.T, peer string, connTs time.Time, gvs interfaces.Knowledge) error
}

// syncer wakes up every peerSyncInterval to do work: (1) Refresh memberView if
// needed and pick a peer from all the known remote peers to sync with. (2) Act
// as an initiator and sync syncgroup metadata for all common syncgroups with
// the chosen peer (getting updates from the remote peer, detecting and
// resolving conflicts) (3) Act as an initiator and sync data corresponding to
// all common syncgroups across all Apps/Databases with the chosen peer; (4)
// Fetch any queued blobs. (5) Transfer ownership of blobs if needed. (6) Act as
// a syncgroup publisher to publish pending syncgroups; (6) Garbage collect
// older generations.
//
// TODO(hpucha): Currently only does initiation. Add rest.
func (s *syncService) syncer(ctx *context.T) {
	defer s.pending.Done()

	ticker := time.NewTicker(peerSyncInterval)
	defer ticker.Stop()

	for !s.Closed() {
		select {
		case <-ticker.C:
			if s.Closed() {
				break
			}
			s.syncerWork(ctx)

		case <-s.closed:
			break
		}
	}
	vlog.VI(1).Info("sync: syncer: channel closed, stop work and exit")
}

func (s *syncService) syncerWork(ctx *context.T) {
	// TODO(hpucha): Cut a gen for the responder even if there is no
	// one to initiate to?

	// Do work.
	attemptTs := time.Now()
	peer, err := s.ps.pickPeer(ctx)
	if err != nil {
		return
	}

	err = s.syncVClock(ctx, peer)
	// Abort syncing if there is a connection error with peer.
	if verror.ErrorID(err) != interfaces.ErrConnFail.ID {
		err = s.getDeltasFromPeer(ctx, peer)
	}

	s.ps.updatePeerFromSyncer(ctx, peer, attemptTs, verror.ErrorID(err) == interfaces.ErrConnFail.ID)
}

////////////////////////////////////////
// Peer selection policies.

func (s *syncService) newPeerSelector(ctx *context.T, peerSelectionPolicy int) error {
	switch peerSelectionPolicy {
	case selectRandom:
		s.ps = &randomPeerSelector{s: s}
		return nil
	case selectNeighborhoodAware:
		s.ps = &neighborhoodAwarePeerSelector{
			s:       s,
			peerTbl: make(map[string]*peerSyncInfo),
		}
		return nil
	default:
		return verror.New(verror.ErrInternal, ctx, "unknown peer selection policy")
	}
}

////////////////////////////////////////
// Random selector.

type randomPeerSelector struct {
	s *syncService
}

func (ps *randomPeerSelector) pickPeer(ctx *context.T) (connInfo, error) {
	return ps.s.pickPeerRandom(ctx)
}

func (s *syncService) pickPeerRandom(ctx *context.T) (connInfo, error) {
	var peer connInfo

	members := s.getMembers(ctx)
	// Remove myself from the set.
	delete(members, s.name)
	if len(members) == 0 {
		return peer, verror.New(verror.ErrInternal, ctx, "no useful peer")
	}

	// Pick a peer at random.
	ind := randIntn(len(members))
	for m := range members {
		if ind == 0 {
			peer.relName = m
			return peer, nil
		}
		ind--
	}
	vlog.Fatalf("random selection didn't succeed")
	return peer, verror.New(verror.ErrInternal, ctx, "random selection didn't succeed")
}

func (ps *randomPeerSelector) updatePeerFromSyncer(ctx *context.T, peer connInfo, attemptTs time.Time, failed bool) error {
	// Random selector does not care about this information.
	return nil
}

func (ps *randomPeerSelector) updatePeerFromResponder(ctx *context.T, peer string, connTs time.Time, gvs interfaces.Knowledge) error {
	// Random selector does not care about this information.
	return nil
}

////////////////////////////////////////
// NeighborhoodAware selector.

// peerSyncInfo is the running statistics collected per peer, for a peer which
// syncs with this node or with which this node syncs with.
type peerSyncInfo struct {
	// Number of continuous failures noticed when attempting to connect with
	// this peer, either via any of its advertised mount tables or via
	// neighborhood. These counters are reset when the connection to the
	// peer succeeds.
	numFailuresMountTable   uint64
	numFailuresNeighborhood uint64

	// The most recent timestamp when a connection to this peer was attempted.
	attemptTs time.Time
	// The most recent timestamp when a connection to this peer succeeded.
	successTs time.Time
	// The most recent timestamp when this peer synced with this node.
	fromTs time.Time
	// Map of database names and their corresponding generation vectors for
	// data and syncgroups.
	gvs map[string]interfaces.Knowledge
}

type neighborhoodAwarePeerSelector struct {
	sync.RWMutex
	s *syncService
	// In-memory cache of information relevant to syncing with a peer. This
	// information could potentially be used in peer selection.
	numFailuresMountTable uint64 // total number of mount table failures across peers.
	curCount              uint64 // remaining number of sync rounds before a mount table will be retried.
	peerTbl               map[string]*peerSyncInfo
}

// pickPeer selects a peer to sync with by incorporating the neighborhood
// information. The heuristic bootstraps by picking a random peer for each
// iteration and syncing with it via one of the syncgroup mount tables. This
// continues until a peer is unreachable via the mount table (ErrConnFail). Upon
// a connection error, the heuristic switches to pick a random peer from the
// neighborhood while still probing to see if peers are reachable via the
// syncgroup mount table using an exponential backoff. For example, when the
// connection fails for the first time, the heuristic waits for two sync rounds
// before contacting a peer via a mount table again. If this attempt also fails,
// it then backs off to wait four sync rounds before trying the mount table, and
// so on. Upon success, these counters are reset, and the heuristic goes back to
// randomly selecting a peer and communicating via the syncgroup mount tables.
//
// This is a simple heuristic to begin incorporating neighborhood information
// into peer selection. Its drawbacks include: (a) A single peer being
// unreachable assumes connectivity issues, and switches to
// neighborhood. However, this may be an isolated issue with just the one
// peer. (b) When there are no connectivity issues, the heuristic does not use
// the neighborhood information for the peers. We may wish to reconsider this
// after we have sufficient confidence that going over the neighborhood is
// faster than connecting via the mount table. (c) The heuristic is stateless
// across invocations. The same peer may be selected back-to-back to sync via
// neighborhood and via the mount table. (d) Peers are still randomly selected
// in each mode of operation.
func (ps *neighborhoodAwarePeerSelector) pickPeer(ctx *context.T) (connInfo, error) {
	ps.Lock()
	defer ps.Unlock()

	if ps.curCount == 0 {
		vlog.VI(4).Infof("sync: pickPeer: picking from all sgmembers")

		// Pick a peer at random.
		return ps.s.pickPeerRandom(ctx)
	}

	ps.curCount--

	var peer connInfo
	members := ps.s.getMembers(ctx)

	// Remove myself from the set.
	delete(members, ps.s.name)
	if len(members) == 0 {
		return peer, verror.New(verror.ErrInternal, ctx, "no useful peer")
	}

	// Pick a peer from the neighborhood if available.
	neighbors := ps.s.filterDiscoveryPeers(members)
	if len(neighbors) == 0 {
		vlog.VI(4).Infof("sync: pickPeer: no sgneighbors found")
		return peer, verror.New(verror.ErrInternal, ctx, "no useful peer")
	}

	vlog.VI(4).Infof("sync: pickPeer: picking from sgneighbors")

	// Pick a peer at random.
	ind := randIntn(len(neighbors))
	for n, svc := range neighbors {
		if ind == 0 {
			peer.relName = n
			peer.addrs = svc.Addrs
			return peer, nil
		}
		ind--
	}
	vlog.Fatalf("random selection didn't succeed")
	return peer, verror.New(verror.ErrInternal, ctx, "peer selection didn't succeed")
}

func (ps *neighborhoodAwarePeerSelector) updatePeerFromSyncer(ctx *context.T, peer connInfo, attemptTs time.Time, failed bool) error {
	ps.Lock()
	defer ps.Unlock()

	info, ok := ps.peerTbl[peer.relName]
	if !ok {
		info = &peerSyncInfo{}
		ps.peerTbl[peer.relName] = info
	}

	info.attemptTs = attemptTs
	if !failed {
		if peer.addrs != nil {
			info.numFailuresNeighborhood = 0
		} else {
			info.numFailuresMountTable = 0
			ps.numFailuresMountTable = 0
		}

		info.successTs = time.Now()
		return nil
	}

	if peer.addrs != nil {
		info.numFailuresNeighborhood++
	} else {
		ps.numFailuresMountTable++
		ps.curCount = roundsToBackoff(ps.numFailuresMountTable)

		info.numFailuresMountTable++
	}

	return nil
}

// roundsToBackoff computes the exponential backoff with a cap on the backoff.
func roundsToBackoff(failures uint64) uint64 {
	if failures >= maxFailuresForBackoff {
		failures = maxFailuresForBackoff
	}

	return 1 << failures
}

func (ps *neighborhoodAwarePeerSelector) updatePeerFromResponder(ctx *context.T, peer string, connTs time.Time, gv interfaces.Knowledge) error {
	return nil
}
