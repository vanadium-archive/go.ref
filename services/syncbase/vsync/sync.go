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

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

// syncService contains the metadata for the sync module.
type syncService struct {
	id int64 // globally unique id for this instance of Syncbase
	sv interfaces.Service
	// State to coordinate shutdown of spawned goroutines.
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
// Concurrency: sync initializes two goroutines at startup: a "watcher" and an
// "initiator". The "watcher" thread is responsible for watching the store for
// changes to its objects. The "initiator" thread is responsible for
// periodically contacting peers to fetch changes from them. In addition, the
// sync module responds to incoming RPCs from remote sync modules.
func New(ctx *context.T, call rpc.ServerCall, sv interfaces.Service) (*syncService, error) {
	s := &syncService{sv: sv}

	data := &syncData{}
	if err := util.GetObject(sv.St(), s.StKey(), data); err != nil {
		if verror.ErrorID(err) != store.ErrUnknownKey.ID {
			return nil, verror.New(verror.ErrInternal, ctx, err)
		}
		// First invocation of vsync.New().
		// TODO(sadovsky): Maybe move guid generation and storage to serviceData.
		data.Id = rng.Int63()
		if err := util.PutObject(sv.St(), s.StKey(), data); err != nil {
			return nil, verror.New(verror.ErrInternal, ctx, err)
		}
	}

	// data.Id is now guaranteed to be initialized.
	s.id = data.Id

	// Channel to propagate close event to all threads.
	s.closed = make(chan struct{})
	s.pending.Add(2)

	// Start watcher thread to watch for updates to local store.
	go s.watchStore()

	// Start initiator thread to periodically get deltas from peers.
	go s.contactPeers()

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
