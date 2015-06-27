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
	"fmt"
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
	// TODO(hpucha): see if "v.io/v23/uniqueid" is a better fit. It is 128 bits.
	id   uint64 // globally unique id for this instance of Syncbase.
	name string // name derived from the global id.
	sv   interfaces.Service

	// State to coordinate shutdown of spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}

	// TODO(hpucha): Other global names to advertise to enable Syncbase
	// discovery. For example, every Syncbase must be reachable under
	// <mttable>/<syncbaseid> for p2p sync. This is the name advertised
	// during SyncGroup join. In addition, a Syncbase might also be
	// accepting "publish SyncGroup requests", and might use a more
	// human-readable name such as <mttable>/<idp>/<sgserver>. All these
	// names must be advertised in the appropriate mount tables.

	// In-memory sync membership info aggregated across databases.
	allMembers *memberView

	// In-memory sync state per Database. This state is populated at
	// startup, and periodically persisted by the initiator.
	syncState     map[string]*dbSyncStateInMem
	syncStateLock sync.Mutex // lock to protect access to the sync state.

	// In-memory tracking of batches during their construction.
	// The sync Initiator and Watcher build batches incrementally here
	// and then persist them in DAG batch entries.  The mutex guards
	// access to the batch set.
	batchesLock sync.Mutex
	batches     batchSet
}

// syncDatabase contains the metadata for syncing a database. This struct is
// used as a receiver to hand off the app-initiated SyncGroup calls that arrive
// against a nosql.Database to the sync module.
type syncDatabase struct {
	db interfaces.Database
}

var (
	rng     = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	rngLock sync.Mutex
	_       interfaces.SyncServerMethods = (*syncService)(nil)
	_       util.Layer                   = (*syncService)(nil)
)

// rand64 generates an unsigned 64-bit pseudo-random number.
func rand64() uint64 {
	rngLock.Lock()
	defer rngLock.Unlock()
	return (uint64(rng.Int63()) << 1) | uint64(rng.Int63n(2))
}

// randIntn generates as an int, a non-negative pseudo-random number in [0,n).
func randIntn(n int) int {
	rngLock.Lock()
	defer rngLock.Unlock()
	return rng.Intn(n)
}

// New creates a new sync module.
//
// Concurrency: sync initializes two goroutines at startup: a "watcher" and an
// "initiator". The "watcher" thread is responsible for watching the store for
// changes to its objects. The "initiator" thread is responsible for
// periodically contacting peers to fetch changes from them. In addition, the
// sync module responds to incoming RPCs from remote sync modules.
func New(ctx *context.T, call rpc.ServerCall, sv interfaces.Service) (*syncService, error) {
	s := &syncService{
		sv:      sv,
		batches: make(batchSet),
	}

	data := &syncData{}
	if err := util.GetObject(sv.St(), s.StKey(), data); err != nil {
		if verror.ErrorID(err) != store.ErrUnknownKey.ID {
			return nil, verror.New(verror.ErrInternal, ctx, err)
		}
		// First invocation of vsync.New().
		// TODO(sadovsky): Maybe move guid generation and storage to serviceData.
		data.Id = rand64()
		if err := util.PutObject(sv.St(), s.StKey(), data); err != nil {
			return nil, verror.New(verror.ErrInternal, ctx, err)
		}
	}

	// data.Id is now guaranteed to be initialized.
	s.id = data.Id
	s.name = fmt.Sprintf("%x", s.id)

	// Initialize in-memory state for the sync module before starting any threads.
	if err := s.initSync(ctx); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}

	// Channel to propagate close event to all threads.
	s.closed = make(chan struct{})
	s.pending.Add(2)

	// Start watcher thread to watch for updates to local store.
	go s.watchStore(ctx)

	// Start initiator thread to periodically get deltas from peers.
	go s.syncer(ctx)

	return s, nil
}

// Close cleans up sync state.
// TODO(hpucha): Hook it up to server shutdown of syncbased.
func (s *syncService) Close() {
	close(s.closed)
	s.pending.Wait()
}

func NewSyncDatabase(db interfaces.Database) *syncDatabase {
	return &syncDatabase{db: db}
}

////////////////////////////////////////
// Core sync method.

func (s *syncService) GetDeltas(ctx *context.T, call interfaces.SyncGetDeltasServerCall) error {
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
