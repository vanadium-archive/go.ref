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

	// peerSelectionPolicy is the policy used to select a peer when
	// the initiator gets a chance to sync.
	peerSelectionPolicy = selectRandom

	// We wait for connectionTimeOut duration for a connection to be
	// established with a peer reachable via a chosen mount table.
	connectionTimeOut = 2 * time.Second
)

// connInfo holds the information needed to connect to a peer.
//
// TODO(hpucha): Add hints to decide if both neighborhood and mount table must
// be tried. Currently, if the addr is set, only the addr is tried.
type connInfo struct {
	// Name of the peer relative to the mount table chosen by the syncgroup
	// creator.
	relName string
	// Network address of the peer if available. For example, this can be
	// obtained from neighborhood discovery.
	addr string
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
	updatePeerFromResponder(ctx *context.T, peer string, connTs time.Time, gv interfaces.GenVector) error
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

	s.newPeerSelector(ctx)
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

	err = s.syncClock(ctx, peer)
	// Abort syncing if there is a connection error with peer.
	if verror.ErrorID(err) != interfaces.ErrConnFail.ID {
		err = s.getDeltasFromPeer(ctx, peer)
	}

	s.ps.updatePeerFromSyncer(ctx, peer, attemptTs, verror.ErrorID(err) == interfaces.ErrConnFail.ID)
}

////////////////////////////////////////
// Peer selection policies.

func (s *syncService) newPeerSelector(ctx *context.T) error {
	switch peerSelectionPolicy {
	case selectRandom:
		s.ps = &randomPeerSelector{s: s}
		return nil
	case selectNeighborhoodAware:
		s.ps = &neighborhoodAwarePeerSelector{s: s}
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
	var peer connInfo

	members := ps.s.getMembers(ctx)
	// Remove myself from the set.
	delete(members, ps.s.name)
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
	return peer, verror.New(verror.ErrInternal, ctx, "random selection didn't succeed")
}

func (ps *randomPeerSelector) updatePeerFromSyncer(ctx *context.T, peer connInfo, attemptTs time.Time, failed bool) error {
	// Random selector does not care about this information.
	return nil
}

func (ps *randomPeerSelector) updatePeerFromResponder(ctx *context.T, peer string, connTs time.Time, gv interfaces.GenVector) error {
	// Random selector does not care about this information.
	return nil
}

////////////////////////////////////////
// NeighborhoodAware selector.

// peerSyncInfo is the running statistics collected per peer, for a peer which
// syncs with this node or with which this node syncs with.
type peerSyncInfo struct {
	// Number of continuous failures noticed when attempting to connect with
	// this peer, either via its advertised mount table or via
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
	gvs map[string]interfaces.GenVector
}

type neighborhoodAwarePeerSelector struct {
	s *syncService
	// In-memory cache of information relevant to syncing with a peer. This
	// information could potentially be used in peer selection.
	peerTbl     map[string]*peerSyncInfo
	peerTblLock sync.RWMutex
}

func (ps *neighborhoodAwarePeerSelector) pickPeer(ctx *context.T) (connInfo, error) {
	var peer connInfo
	return peer, nil
}

func (ps *neighborhoodAwarePeerSelector) updatePeerFromSyncer(ctx *context.T, peer connInfo, attemptTs time.Time, failed bool) error {
	ps.peerTblLock.Lock()
	defer ps.peerTblLock.Unlock()

	info, ok := ps.peerTbl[peer.relName]
	if !ok {
		info = &peerSyncInfo{}
		ps.peerTbl[peer.relName] = info
	}

	info.attemptTs = attemptTs
	if !failed {
		info.numFailuresMountTable = 0
		info.numFailuresNeighborhood = 0
		info.successTs = time.Now()
		return nil
	}

	if peer.addr != "" {
		info.numFailuresNeighborhood++
	} else {
		info.numFailuresMountTable++
	}

	return nil
}

func (ps *neighborhoodAwarePeerSelector) updatePeerFromResponder(ctx *context.T, peer string, connTs time.Time, gv interfaces.GenVector) error {
	return nil
}
