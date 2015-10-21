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
	"container/list"
	"fmt"
	"math/rand"
	"path"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/services/syncbase/nosql"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/clock"
	blob "v.io/x/ref/services/syncbase/localblobstore"
	fsblob "v.io/x/ref/services/syncbase/localblobstore/fs_cablobstore"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// syncService contains the metadata for the sync module.
type syncService struct {
	// TODO(hpucha): see if "v.io/v23/uniqueid" is a better fit. It is 128
	// bits. Another alternative is to derive this from the blessing of
	// Syncbase. Syncbase can append a uuid to the blessing it is given upon
	// launch and use its hash as id. Note we cannot use public key since we
	// want to support key rollover.
	id   uint64 // globally unique id for this instance of Syncbase.
	name string // name derived from the global id.
	sv   interfaces.Service

	// Root context to be used to create a context for advertising over
	// neighborhood.
	ctx *context.T

	// Cancel function for a context derived from the root context when
	// advertising over neighborhood. This is needed to stop advertising.
	advCancel context.CancelFunc

	nameLock sync.Mutex // lock needed to serialize adding and removing of Syncbase names.

	// High-level lock to serialize the watcher and the initiator. This lock is
	// needed to handle the following cases: (a) When the initiator is
	// cutting a local generation, it waits for the watcher to commit the
	// latest local changes before including them in the checkpoint. (b)
	// When the initiator is receiving updates, it reads the latest head of
	// an object as per the DAG state in order to construct the in-memory
	// graft map used for conflict detection. At the same time, if a watcher
	// is processing local updates, it may move the object head. Hence the
	// initiator and watcher contend on the DAG head of an object. Instead
	// of retrying a transaction which causes the entire delta to be
	// replayed, we use pessimistic locking to serialize the initiator and
	// the watcher.
	//
	// TODO(hpucha): This is a temporary hack.
	thLock sync.RWMutex

	// State to coordinate shutdown of spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}

	// TODO(hpucha): Other global names to advertise to enable Syncbase
	// discovery. For example, every Syncbase must be reachable under
	// <mttable>/<syncbaseid> for p2p sync. This is the name advertised
	// during syncgroup join. In addition, a Syncbase might also be
	// accepting "publish syncgroup requests", and might use a more
	// human-readable name such as <mttable>/<idp>/<sgserver>. All these
	// names must be advertised in the appropriate mount tables.

	// In-memory sync membership info aggregated across databases.
	allMembers     *memberView
	allMembersLock sync.RWMutex

	// In-memory map of sync peers found in the neighborhood through the
	// discovery service.  The map key is the discovery service UUID.
	discoveryPeers     map[string]*discovery.Service
	discoveryPeersLock sync.RWMutex

	// In-memory sync state per Database. This state is populated at
	// startup, and periodically persisted by the initiator.
	syncState     map[string]*dbSyncStateInMem
	syncStateLock sync.Mutex // lock to protect access to the sync state.

	// In-memory queue of syncgroups to be published.  It is reconstructed
	// at startup from syncgroup info so it does not need to be persisted.
	sgPublishQueue     *list.List
	sgPublishQueueLock sync.Mutex

	// In-memory tracking of batches during their construction.
	// The sync Initiator and Watcher build batches incrementally here
	// and then persist them in DAG batch entries.  The mutex guards
	// access to the batch set.
	batchesLock sync.Mutex
	batches     batchSet

	// Metadata related to blob handling.
	bst           blob.BlobStore                 // local blob store associated with this Syncbase.
	blobDirectory map[nosql.BlobRef]*blobLocInfo // directory structure containing blob location information.
	blobDirLock   sync.RWMutex                   // lock to synchronize access to the blob directory information.

	// Syncbase clock related variables.
	vclock *clock.VClock

	// Peer selector for picking a peer to sync with.
	ps peerSelector
}

// syncDatabase contains the metadata for syncing a database. This struct is
// used as a receiver to hand off the app-initiated syncgroup calls that arrive
// against a nosql.Database to the sync module.
type syncDatabase struct {
	db   interfaces.Database
	sync interfaces.SyncServerMethods
}

var (
	rng     = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	rngLock sync.Mutex
	_       interfaces.SyncServerMethods = (*syncService)(nil)
)

// rand64 generates an unsigned 64-bit pseudo-random number.
func rand64() uint64 {
	rngLock.Lock()
	defer rngLock.Unlock()
	return (uint64(rng.Int63()) << 1) | uint64(rng.Int63n(2))
}

// randIntn mimics rand.Intn (generates a non-negative pseudo-random number in [0,n)).
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
func New(ctx *context.T, call rpc.ServerCall, sv interfaces.Service, blobStEngine, blobRootDir string, vclock *clock.VClock) (*syncService, error) {
	s := &syncService{
		sv:             sv,
		batches:        make(batchSet),
		sgPublishQueue: list.New(),
		vclock:         vclock,
		ctx:            ctx,
	}

	data := &syncData{}
	if err := store.RunInTransaction(sv.St(), func(tx store.Transaction) error {
		if err := util.Get(ctx, sv.St(), s.stKey(), data); err != nil {
			if verror.ErrorID(err) != verror.ErrNoExist.ID {
				return err
			}
			// First invocation of vsync.New().
			// TODO(sadovsky): Maybe move guid generation and storage to serviceData.
			data.Id = rand64()
			return util.Put(ctx, tx, s.stKey(), data)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// data.Id is now guaranteed to be initialized.
	s.id = data.Id
	s.name = syncbaseIdToName(s.id)

	// Initialize in-memory state for the sync module before starting any threads.
	if err := s.initSync(ctx); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}

	// Open a blob store.
	var err error
	s.bst, err = fsblob.Create(ctx, blobStEngine, path.Join(blobRootDir, "blobs"))
	if err != nil {
		return nil, err
	}
	s.blobDirectory = make(map[nosql.BlobRef]*blobLocInfo)

	// Channel to propagate close event to all threads.
	s.closed = make(chan struct{})
	s.pending.Add(3)

	// Start watcher thread to watch for updates to local store.
	go s.watchStore(ctx)

	// Start initiator thread to periodically get deltas from peers.
	go s.syncer(ctx)

	// Start the discovery service thread to listen to neighborhood updates.
	go s.discoverPeers(ctx)

	return s, nil
}

// Closed returns true if the sync service channel is closed indicating that the
// service is shutting down.
func (s *syncService) Closed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// discoverPeers listens to updates from the discovery service to learn about
// sync peers as they enter and leave the neighborhood.
func (s *syncService) discoverPeers(ctx *context.T) {
	defer s.pending.Done()

	scanner := v23.GetDiscovery(ctx)
	if scanner == nil {
		vlog.Fatal("sync: discoverPeers: discovery service not initialized")
	}

	// TODO(rdaoud): refactor this interface name query string.
	query := interfaces.SyncDesc.PkgPath + "/" + interfaces.SyncDesc.Name
	ch, err := scanner.Scan(ctx, query)
	if err != nil {
		vlog.Errorf("sync: discoverPeers: cannot start discovery service: %v", err)
		return
	}

	for !s.Closed() {
		select {
		case update, ok := <-ch:
			if s.Closed() {
				break
			}
			if !ok {
				vlog.VI(1).Info("sync: discoverPeers: scan cancelled, stop listening and exit")
				return
			}
			switch u := update.(type) {
			case discovery.UpdateFound:
				svc := &u.Value.Service
				s.updateDiscoveryPeer(string(svc.InstanceUuid), svc)
			case discovery.UpdateLost:
				s.updateDiscoveryPeer(string(u.Value.InstanceUuid), nil)
			default:
				vlog.Errorf("sync: discoverPeers: ignoring invalid update: %v", update)
			}

		case <-s.closed:
			break
		}
	}

	vlog.VI(1).Info("sync: discoverPeers: channel closed, stop listening and exit")
}

// updateDiscoveryPeer adds or removes information about a sync peer found in
// the neighborhood through the discovery service.  If the service entry is nil
// the peer is removed from the discovery map.
func (s *syncService) updateDiscoveryPeer(peerInstance string, service *discovery.Service) {
	s.discoveryPeersLock.Lock()
	defer s.discoveryPeersLock.Unlock()

	if s.discoveryPeers == nil {
		s.discoveryPeers = make(map[string]*discovery.Service)
	}

	if service != nil {
		vlog.VI(3).Infof("sync: updateDiscoveryPeer: adding peer %s: %v", peerInstance, service)
		s.discoveryPeers[peerInstance] = service
	} else {
		vlog.VI(3).Infof("sync: updateDiscoveryPeer: removing peer %s", peerInstance)
		delete(s.discoveryPeers, peerInstance)
	}
}

// AddNames publishes all the names for this Syncbase instance gathered from all
// the syncgroups it is currently participating in. This is needed when
// syncbased is restarted so that it can republish itself at the names being
// used in the syncgroups.
func AddNames(ctx *context.T, ss interfaces.SyncServerMethods, svr rpc.Server) error {
	vlog.VI(2).Infof("sync: AddNames: begin")
	defer vlog.VI(2).Infof("sync: AddNames: end")

	s := ss.(*syncService)
	s.nameLock.Lock()
	defer s.nameLock.Unlock()

	mInfo := s.copyMemberInfo(ctx, s.name)
	if mInfo == nil || len(mInfo.mtTables) == 0 {
		vlog.VI(2).Infof("sync: AddNames: end returning no names")
		return nil
	}

	for mt := range mInfo.mtTables {
		name := naming.Join(mt, s.name)
		if err := svr.AddName(name); err != nil {
			vlog.VI(2).Infof("sync: AddNames: end returning err %v", err)
			return err
		}
	}

	return s.publishInNeighborhood(svr)
}

// publishInNeighborhood checks if the Syncbase service is already being
// advertised over the neighborhood. If not, it begins advertising. The caller
// of the function is holding nameLock.
func (s *syncService) publishInNeighborhood(svr rpc.Server) error {
	// Syncbase is already being advertised.
	if s.advCancel != nil {
		return nil
	}

	ctx, stop := context.WithCancel(s.ctx)

	advertiser := v23.GetDiscovery(ctx)
	if advertiser == nil {
		vlog.Fatal("sync: publishInNeighborhood: discovery not initialized.")
	}

	// TODO(hpucha): For now we grab the current address of the server. This
	// will be replaced by library support that will take care of roaming.
	var eps []string
	for _, ep := range svr.Status().Endpoints {
		eps = append(eps, ep.Name())
	}

	sbService := discovery.Service{
		InstanceUuid:  []byte(s.name),
		InstanceName:  s.name,
		InterfaceName: interfaces.SyncDesc.PkgPath + "/" + interfaces.SyncDesc.Name,
		Addrs:         eps,
	}

	// Duplicate calls to advertise will return an error.
	_, err := advertiser.Advertise(ctx, sbService, nil)
	if err == nil {
		s.advCancel = stop
	}
	return err
}

// Close waits for spawned sync threads (watcher and initiator) to shut down,
// and closes the local blob store handle.
func Close(ss interfaces.SyncServerMethods) {
	vlog.VI(2).Infof("sync: Close: begin")
	defer vlog.VI(2).Infof("sync: Close: end")

	s := ss.(*syncService)
	close(s.closed)
	s.pending.Wait()
	s.bst.Close()
}

func syncbaseIdToName(id uint64) string {
	return fmt.Sprintf("%x", id)
}

func NewSyncDatabase(db interfaces.Database) *syncDatabase {
	return &syncDatabase{db: db, sync: db.App().Service().Sync()}
}

func (s *syncService) stKey() string {
	return util.SyncPrefix
}
