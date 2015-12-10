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
	"strings"
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
	idiscovery "v.io/x/ref/lib/discovery"
	blob "v.io/x/ref/services/syncbase/localblobstore"
	fsblob "v.io/x/ref/services/syncbase/localblobstore/fs_cablobstore"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/vclock"
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

	// In-memory maps of sync peers and syncgroups found in the neighborhood
	// through the discovery service.  For the sync peers map, the keys are
	// the Syncbase names.  For the syncgroups map, the outer-map keys are
	// the syncgroup names, the inner-map keys are the Syncbase names.
	discoveryPeers      map[string]*discovery.Service
	discoverySyncgroups map[string]map[string]*discovery.Service
	discoveryLock       sync.RWMutex

	// Cancel function for a context derived from the root context when
	// advertising over neighborhood. This is needed to stop advertising.
	advCancel context.CancelFunc

	// Whether to enable neighborhood advertising.
	publishInNH bool

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

	// Syncbase vclock related variables.
	vclock *vclock.VClock

	// Peer manager for managing peers to sync with.
	pm peerManager
}

// syncDatabase contains the metadata for syncing a database. This struct is
// used as a receiver to hand off the app-initiated syncgroup calls that arrive
// against a nosql.Database to the sync module.
type syncDatabase struct {
	db   interfaces.Database
	sync interfaces.SyncServerMethods
}

var (
	ifName  = interfaces.SyncDesc.PkgPath + "/" + interfaces.SyncDesc.Name
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
// Concurrency: sync initializes four goroutines at startup: a "watcher", a
// "syncer", a "neighborhood scanner", and a "peer manager". The "watcher"
// thread is responsible for watching the store for changes to its objects. The
// "syncer" thread is responsible for periodically contacting peers to fetch
// changes from them. The "neighborhood scanner" thread continuously scans the
// neighborhood to learn of other Syncbases and syncgroups in its
// neighborhood. The "peer manager" thread continuously maintains viable peers
// that the syncer can pick from. In addition, the sync module responds to
// incoming RPCs from remote sync modules and local clients.
func New(ctx *context.T, sv interfaces.Service, blobStEngine, blobRootDir string, cl *vclock.VClock, publishInNH bool) (*syncService, error) {
	s := &syncService{
		sv:             sv,
		batches:        make(batchSet),
		sgPublishQueue: list.New(),
		vclock:         cl,
		ctx:            ctx,
		publishInNH:    publishInNH,
	}

	data := &SyncData{}
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

	vlog.VI(1).Infof("sync: New: Syncbase ID is %v", s.id)

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
	s.pending.Add(4)

	// Start watcher thread to watch for updates to local store.
	go s.watchStore(ctx)

	// Start initiator thread to periodically get deltas from peers.
	go s.syncer(ctx)

	// Start the discovery service thread to listen to neighborhood updates.
	go s.discoverNeighborhood(ctx)

	// Initialize a peer manager with the peer selection policy.
	s.pm = newPeerManager(ctx, s, selectRandom)

	// Start the peer manager thread to maintain peers viable for syncing.
	go s.pm.managePeers(ctx)

	return s, nil
}

func (s *syncService) Ping(ctx *context.T, call rpc.ServerCall) error {
	vlog.VI(2).Infof("sync: ping: received")
	return nil
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

// discoverNeighborhood listens to updates from the discovery service to learn
// about sync peers and syncgroups (if they have admins in the neighborhood) as
// they enter and leave the neighborhood.
func (s *syncService) discoverNeighborhood(ctx *context.T) {
	defer s.pending.Done()

	scanner := v23.GetDiscovery(ctx)
	if scanner == nil {
		vlog.Fatal("sync: discoverNeighborhood: discovery service not initialized")
	}

	query := `v.InterfaceName="` + ifName + `"`
	ch, err := scanner.Scan(ctx, query)
	if err != nil {
		vlog.Errorf("sync: discoverNeighborhood: cannot start discovery service: %v", err)
		return
	}

	for !s.Closed() {
		select {
		case update, ok := <-ch:
			if !ok || s.Closed() {
				break
			}

			switch u := update.(type) {
			case discovery.UpdateFound:
				svc := &u.Value.Service
				s.updateDiscoveryInfo(svc.InstanceId, svc)
			case discovery.UpdateLost:
				s.updateDiscoveryInfo(u.Value.InstanceId, nil)
			default:
				vlog.Errorf("sync: discoverNeighborhood: ignoring invalid update: %v", update)
			}

		case <-s.closed:
			break
		}
	}

	vlog.VI(1).Info("sync: discoverNeighborhood: channel closed, stop listening and exit")
}

// discoverySyncgroupInstanceId returns the discovery instance ID for a given
// syncgroup and peer.
func discoverySyncgroupInstanceId(sgName, peerName string) string {
	return strings.Join([]string{discoverySyncgroupPrefix, base64Encode(sgName), peerName}, ":")
}

// updateDiscoveryInfo adds or removes information about a sync peer or a
// syncgroup found in the neighborhood through the discovery service.  If the
// service entry is nil the record is removed from its discovery map.  The
// instance ID string encodes the type of information received:
// - "<peerName>" for sync peer info
// - "sg:<sgName>:<peerName>" for syncgroup admin info
// Note: the semicolon separator is safe to use here because both the syncgroup
// name (base64-encoded) and peer name (hex number) do not contain a semicolon.
// The regular Syncbase separator \xfe cannot be used because the discovery
// service requires instance IDs to be valid UTF-8 strings.
func (s *syncService) updateDiscoveryInfo(id string, service *discovery.Service) {
	s.discoveryLock.Lock()
	defer s.discoveryLock.Unlock()

	vlog.VI(3).Infof("sync: updateDiscoveryInfo: %s: %v", id, service)

	if s.discoveryPeers == nil {
		s.discoveryPeers = make(map[string]*discovery.Service)
	}
	if s.discoverySyncgroups == nil {
		s.discoverySyncgroups = make(map[string]map[string]*discovery.Service)
	}

	// Parse the instance ID string to determine the type of discovery info.
	parts := strings.Split(id, ":")
	switch len(parts) {
	case 1:
		// Update the sync peer info.
		if service != nil {
			s.discoveryPeers[parts[0]] = service
		} else {
			delete(s.discoveryPeers, parts[0])
		}

	case 3:
		switch parts[0] {
		case discoverySyncgroupPrefix:
			// Update the syncgroup admin info.
			sgName := base64Decode(parts[1])
			admins := s.discoverySyncgroups[sgName]
			if service != nil {
				if admins == nil {
					admins = make(map[string]*discovery.Service)
					s.discoverySyncgroups[sgName] = admins
				}
				admins[parts[2]] = service
			} else if admins != nil {
				delete(admins, parts[2])
				if len(admins) == 0 {
					delete(s.discoverySyncgroups, sgName)
				}
			}

		default:
			vlog.Errorf("sync: updateDiscoveryInfo: invalid instance ID header: %s", id)
		}

	default:
		vlog.Errorf("sync: updateDiscoveryInfo: invalid instance ID format: %s", id)
	}
}

// filterDiscoveryPeers returns only those peers discovered via neighborhood
// that are also found in sgMembers (passed as input argument).
func (s *syncService) filterDiscoveryPeers(sgMembers map[string]uint32) map[string]*discovery.Service {
	s.discoveryLock.Lock()
	defer s.discoveryLock.Unlock()

	if s.discoveryPeers == nil {
		return nil
	}

	sgNeighbors := make(map[string]*discovery.Service)

	for peer, svc := range s.discoveryPeers {
		if _, ok := sgMembers[peer]; ok {
			sgNeighbors[peer] = svc
		}
	}

	return sgNeighbors
}

// discoverySyncgroupAdmins returns syncgroup admins found in the neighborhood
// via the discovery service.
func (s *syncService) discoverySyncgroupAdmins(sgName string) map[string]*discovery.Service {
	s.discoveryLock.Lock()
	defer s.discoveryLock.Unlock()

	if s.discoverySyncgroups == nil {
		return nil
	}

	sgInfo := s.discoverySyncgroups[sgName]
	if sgInfo == nil {
		return nil
	}

	admins := make(map[string]*discovery.Service)

	for peer, svc := range sgInfo {
		admins[peer] = svc
	}

	return admins
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
	if !s.publishInNH {
		return nil
	}
	// Syncbase is already being advertised.
	if s.advCancel != nil {
		return nil
	}

	sbService := discovery.Service{
		InstanceId:    s.name,
		InterfaceName: ifName,
	}
	ctx, stop := context.WithCancel(s.ctx)

	// Duplicate calls to advertise will return an error.
	_, err := idiscovery.AdvertiseServer(ctx, svr, "", &sbService, nil)

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
