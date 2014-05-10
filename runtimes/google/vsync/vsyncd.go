package vsync

// Package vsync provides veyron sync daemon utility functions. Sync
// daemon serves incoming GetDeltas requests and contacts other peers
// to get deltas from them. When it receives a GetDeltas request, the
// incoming generation vector is diffed with the local generation
// vector, and missing generations are sent back. When it receives
// log records in response to a GetDeltas request, it replays those
// log records to get in sync with the sender.
import (
	"strings"
	"sync"
	"time"

	"veyron/services/store/estore"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vlog"
	"veyron2/vom"
)

const (
	// peerSyncInterval is the duration between two consecutive sync events.
	// In every sync event, syncd contacts all of its peers to obtain any pending updates.
	peerSyncInterval = 10 * time.Second
)

// syncd contains the metadata for the sync daemon.
type syncd struct {
	// Pointers to metadata structures.
	log    *iLog
	devtab *devTable
	dag    *dag

	// Local device id.
	id DeviceID

	// State to contact peers periodically and get deltas.
	// TODO(hpucha): This is an initial version with command line arguments and basic synchronization.
	// Next steps are to enhance this to handle concurrent requests, tie this up into mount table,
	// hook it up to logging etc.
	neighbors   []string
	neighborIDs []string

	// RWlock to concurrently access log and device table data structures.
	// TODO(hpucha): implement synchronization among threads.
	lock sync.RWMutex

	pending sync.WaitGroup
	closed  chan struct{}

	// Local Veyron store.
	vstoreEndpoint string
	vstore         storage.Store

	// Handlers for goroutine procedures.
	hdlGC      *syncGC
	hdlWatcher *syncWatcher
}

// NewSyncd creates a new syncd instance.
//
// Syncd concurrency: syncd initializes three goroutines at
// startup. The "watcher" thread is responsible for watching the store
// for changes to its objects. The "initiator" thread is responsible
// for periodically checking the neighborhood and contacting a peer to
// obtain changes from that peer. The "gc" thread is responsible for
// periodically checking if any log records and dag state can be
// pruned. All these 3 threads perform write operations to the data
// structures, and synchronize by acquiring a write lock on s.lock. In
// addition, when syncd receives an incoming RPC, it responds to the
// request by acquiring a read lock on s.lock. Thus, at any instant in
// time, either one of the watcher, initiator or gc threads is active,
// or any number of responders can be active, serving incoming
// requests. Fairness between these threads follows from
// sync.RWMutex. The spec says that the writers cannot be starved by
// the readers but it does not guarantee FIFO. We may have to revisit
// this in the future.
func NewSyncd(peerEndpoints, peerDeviceIDs, devid, storePath, vstoreEndpoint string) *syncd {
	// Connect to the local Veyron store.
	// At present this is optional to allow testing (from the command-line) w/o Veyron store running.
	// TODO: connecting to Veyron store should be mandatory.
	var st storage.Store
	if vstoreEndpoint != "" {
		vs, err := vstore.New(vstoreEndpoint)
		if err != nil {
			vlog.Fatalf("newSyncd: cannot connect to Veyron store endpoint (%s): %s", vstoreEndpoint, err)
		}
		st = vs
	}

	return newSyncdCore(peerEndpoints, peerDeviceIDs, devid, storePath, vstoreEndpoint, st)
}

// newSyncdCore is the internal function that creates the Syncd structure and initilizes
// its thread (goroutines).  It takes a Veyron Store parameter to separate the core of
// Syncd setup from the external dependency on Veyron Store.
func newSyncdCore(peerEndpoints, peerDeviceIDs, devid, storePath, vstoreEndpoint string, store storage.Store) *syncd {
	s := &syncd{}

	// Bootstrap my own DeviceID.
	s.id = DeviceID(devid)

	var err error
	// Log init.
	if s.log, err = openILog(storePath+"/ilog", s); err != nil {
		vlog.Fatalf("newSyncd: ILogInit failed: err %v", err)
	}

	// DevTable init.
	if s.devtab, err = openDevTable(storePath+"/dtab", s); err != nil {
		vlog.Fatalf("newSyncd: DevTableInit failed: err %v", err)
	}

	// Dag Init.
	if s.dag, err = openDAG(storePath + "/dag"); err != nil {
		vlog.Fatalf("newSyncd: OpenDag failed: err %v", err)
	}

	// Veyron Store.
	s.vstoreEndpoint = vstoreEndpoint
	s.vstore = store

	// Register these Watch data types with VOM.
	// TODO(tilaks): why aren't they auto-retrieved from the IDL?
	vom.Register(&estore.Mutation{})
	vom.Register(&storage.DEntry{})

	// Bootstrap my peer list.
	s.neighbors = strings.Split(peerEndpoints, ",")
	s.neighborIDs = strings.Split(peerDeviceIDs, ",")
	if len(s.neighbors) != len(s.neighborIDs) {
		vlog.Fatalf("newSyncd: Mismatch between number of endpoints and IDs")
	}
	vlog.VI(1).Infof("newSyncd: My device ID: %s\n", s.id)
	vlog.VI(1).Infof("newSyncd: Peer endpoints: %v\n", s.neighbors)
	vlog.VI(1).Infof("newSyncd: Peer IDs: %v\n", s.neighborIDs)
	vlog.VI(1).Infof("newSyncd: Local Veyron store: %s\n", s.vstoreEndpoint)

	// Channel to propagate close event to all threads.
	s.closed = make(chan struct{})

	s.pending.Add(3)
	// Get deltas every peerSyncInterval.
	go s.contactPeers()

	// Garbage collect every garbageCollectInterval.
	s.hdlGC = newGC(s)
	go s.hdlGC.garbageCollect()

	// Start a watcher thread that will get updates from local store.
	s.hdlWatcher = newWatcher(s)
	go s.hdlWatcher.watchStore()

	return s
}

// contactPeers wakes up every peerSyncInterval to contact peers and get deltas from them.
func (s *syncd) contactPeers() {
	ticker := time.NewTicker(peerSyncInterval)
	for {
		select {
		case <-s.closed:
			ticker.Stop()
			s.pending.Done()
			return
		case <-ticker.C:
		}

		for i, ep := range s.neighbors {
			if ep == "" {
				continue
			}
			s.GetDeltasFromPeer(ep, s.neighborIDs[i])
		}
	}
}

// Close cleans up syncd state.
func (s *syncd) Close() {
	close(s.closed)
	s.pending.Wait()

	// TODO(hpucha): close without flushing.
}

// isSyncClosing returns true if Close() was called i.e. the "closed" channel is closed.
func (s *syncd) isSyncClosing() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// GetDeltasFromPeer contacts the specified endpoint to obtain deltas wrt its current generation vector.
func (s *syncd) GetDeltasFromPeer(ep, dID string) {
	vlog.VI(1).Infof("GetDeltasFromPeer:: From server %s with DeviceID %s at %v", ep, dID, time.Now().UTC())

	// Construct a new stub that binds to peer endpoint.
	c, err := BindSync(naming.JoinAddressName(ep, "sync"))
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error binding to server: err %v", err)
	}

	// Send the local generation vector.
	s.lock.RLock()
	local, err := s.devtab.getDevInfo(s.id)
	if err != nil {
		s.lock.RUnlock()
		vlog.Fatalf("GetDeltasFromPeer:: error obtaining local gen vector: err %v", err)
	}
	s.lock.RUnlock()

	vlog.VI(1).Infof("GetDeltasFromPeer:: Sending local information: %v %v", local, local.Vector)

	// Issue a GetDeltas() rpc.
	stream, err := c.GetDeltas(local.Vector, s.id)
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error getting deltas: err %v", err)
	}

	minGens, err := s.log.processLogStream(stream)
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error processing logs: err %v", err)
	}

	remotevec, err := stream.Finish()
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: finish failed with err %v", err)
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	// Update the local gen vector and put it in kvdb.
	if err = s.devtab.updateLocalGenVector(local.Vector, remotevec); err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: UpdateLocalGenVector failed with err %v", err)
	}

	if err = s.devtab.putDevInfo(s.id, local); err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: PutDevInfo failed with err %v", err)
	}

	if err := s.devtab.updateReclaimVec(minGens); err != nil {
		vlog.Fatalf("updateReclaimVec:: updateReclaimVec failed with err %v", err)
	}

	// Cache the remote generation vector for space reclamation.
	if err = s.devtab.putGenVec(DeviceID(dID), remotevec); err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: PutGenVec failed with err %v", err)
	}

	vlog.VI(1).Infof("GetDeltasFromPeer:: Local %v vector %v", local, local.Vector)
	vlog.VI(1).Infof("GetDeltasFromPeer:: Remote vector %v", remotevec)
}

// GetDeltas responds to the incoming request from a client by sending missing generations to the client.
func (s *syncd) GetDeltas(_ ipc.Context, In GenVector, ClientID DeviceID, Stream SyncServiceGetDeltasStream) (GenVector, error) {
	vlog.VI(1).Infof("GetDeltas:: Received vector %v from client %s", In, ClientID)

	s.lock.Lock()

	// Note that the incoming client generation vector cannot be
	// used for garbage collection. We can only garbage collect
	// based on the generations we receive from other
	// devices. Receiving a set of generations assures that all
	// updates branching from those generations are also received
	// and hence generations present on all devices can be
	// GC'ed. This function sends generations to other devices and
	// hence does not use the generation vector for GC.
	//
	// TODO(hpucha): Cache the client's incoming generation vector
	// to assist in tracking missing generations and hence next
	// peer to contact.
	if !s.devtab.hasDevInfo(ClientID) {
		if err := s.devtab.addDevice(ClientID); err != nil {
			s.lock.Unlock()
			vlog.Fatalf("GetDeltas:: addDevice failed with err %v", err)
		}
	}

	// TODO(hpucha): Hack, fills fake log and dag state for testing.
	s.log.fillFakeWatchRecords()

	// Create a new local generation if there are any local updates.
	gen, err := s.log.createLocalGeneration()
	if err == nil {
		// Update local generation vector in devTable.
		if err = s.devtab.updateGeneration(s.id, s.id, gen); err != nil {
			s.lock.Unlock()
			vlog.Fatalf("GetDeltas:: UpdateGeneration failed with err %v", err)
		}
	} else if err == errNoUpdates {
		vlog.VI(1).Infof("GetDeltas:: No new updates. Local at %d", gen)
	} else {
		s.lock.Unlock()
		vlog.Fatalf("GetDeltas:: CreateLocalGeneration failed with err %v", err)
	}

	// Get local generation vector.
	out, err := s.devtab.getGenVec(s.id)
	if err != nil {
		s.lock.Unlock()
		vlog.Fatalf("GetDeltas:: GetGenVec failed with err %v", err)
	}
	s.lock.Unlock()

	s.lock.RLock()
	defer s.lock.RUnlock()

	// Diff the two generation vectors.
	gens, err := s.devtab.diffGenVectors(out, In)
	if err != nil {
		vlog.Fatalf("GetDeltas:: Diffing gen vectors failed: err %v", err)
	}

	for _, v := range gens {
		// Sending one generation at a time.
		gen, err := s.log.getGenMetadata(v.devID, v.genID)
		if err != nil || gen.Count <= 0 {
			vlog.Fatalf("GetDeltas:: getGenMetadata failed for generation %s %d %v, err %v",
				v.devID, v.genID, gen, err)
		}

		var count uint64
		for i := LSN(0); i <= gen.MaxLSN; i++ {
			count++
			rec, err := s.log.getLogRec(v.devID, v.genID, i)
			if err != nil {
				vlog.Fatalf("GetDeltas:: Couldn't get log record %s %d %d, err %v",
					v.devID, v.genID, i, err)
			}
			vlog.VI(1).Infof("Sending log record %v", rec)
			s.lock.RUnlock()
			if err := Stream.Send(*rec); err != nil {
				vlog.Errorf("GetDeltas:: Couldn't send stream err: %v", err)
				s.lock.RLock()
				return GenVector{}, err
			}
			s.lock.RLock()
		}
		if count != gen.Count {
			vlog.Fatalf("GetDeltas:: GenMetadata has incorrect log records for generation %s %d %v",
				v.devID, v.genID, gen)
		}
	}

	return out, nil
}
