// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Package vsync provides sync functionality for Syncbase. Sync
// service serves incoming GetDeltas requests and contacts other peers
// to get deltas from them. When it receives a GetDeltas request, the
// incoming generation vector is diffed with the local generation
// vector, and missing generations are sent back. When it receives log
// records in response to a GetDeltas request, it replays those log
// records to get in sync with the sender.
import (
	"math/rand"
	"sync"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

// syncService contains the metadata for the sync module.
type syncService struct {
	// Globally unique Syncbase id.
	id int64

	// Store for persisting Sync metadata.
	st store.Store

	// State to coordinate shutting down all spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}
}

var (
	rng *rand.Rand
	_   util.Layer = (*syncService)(nil)
)

func init() {
	rng = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
}

// New creates a new sync module.
//
// Sync concurrency: sync initializes two goroutines at startup. The
// "watcher" thread is responsible for watching the store for changes
// to its objects. The "initiator" thread is responsible for
// periodically contacting a peer to obtain changes from that peer. In
// addition, the sync module responds to incoming RPCs from remote
// sync modules.
func New(ctx *context.T, call rpc.ServerCall, st store.Store) (*syncService, error) {
	// TODO(hpucha): Add restartability.

	// First invocation of sync.
	s := &syncService{
		id: rng.Int63(),
		st: st,
	}

	// Persist sync metadata.
	data := &syncData{
		Id: s.id,
	}

	if err := util.Put(ctx, call, s.st, s, data); err != nil {
		return nil, err
	}

	// Channel to propagate close event to all threads.
	s.closed = make(chan struct{})
	s.pending.Add(2)

	// Get deltas every so often.
	go s.contactPeers()

	// Start a watcher thread that will get updates from local store.
	go s.watchStore()

	return s, nil
}

////////////////////////////////////////
// Core sync method.

func (s *syncService) GetDeltas(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

////////////////////////////////////////
// util.Layer methods.

func (s *syncService) Name() string {
	return "sync"
}

func (s *syncService) StKey() string {
	return util.SyncPrefix
}
