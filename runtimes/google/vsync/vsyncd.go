package vsync

// Package vsync provides veyron sync daemon utility functions. Sync
// daemon serves incoming GetDeltas requests and contacts other peers
// to get deltas from them. When it receives a GetDeltas request, the
// incoming generation vector is diffed with the local generation
// vector, and missing generations are sent back. When it receives
// log records in response to a GetDeltas request, it replays those
// log records to get in sync with the sender.
import (
	"fmt"
	"strings"
	"sync"
	"time"

	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/vlog"
	"veyron2/vom"

	_ "veyron/services/store/typeregistryhack"
)

// syncd contains the metadata for the sync daemon.
type syncd struct {
	// Pointers to metadata structures.
	log    *iLog
	devtab *devTable
	dag    *dag

	// Local device id.
	id DeviceID

	// RWlock to concurrently access log and device table data structures.
	lock sync.RWMutex
	// State to coordinate shutting down all spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}

	// Local Veyron store.
	storeEndpoint string
	store         raw.Store

	// Handlers for goroutine procedures.
	hdlGC        *syncGC
	hdlWatcher   *syncWatcher
	hdlInitiator *syncInitiator
}

type syncDispatcher struct {
	server ipc.Invoker
	auth   security.Authorizer
}

// NewSyncDispatcher returns an object dispatcher.
func NewSyncDispatcher(s interface{}, auth security.Authorizer) ipc.Dispatcher {
	return &syncDispatcher{ipc.ReflectInvoker(s), auth}
}

func (d *syncDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	if strings.HasSuffix(suffix, "sync") {
		return d.server, d.auth, nil
	}
	return nil, nil, fmt.Errorf("Lookup:: failed on suffix: %s", suffix)
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
func NewSyncd(peerEndpoints, peerDeviceIDs, devid, storePath, storeEndpoint string, syncTick time.Duration) *syncd {
	// Connect to the local Veyron store.
	// At present this is optional to allow testing (from the command-line) w/o Veyron store running.
	// TODO: connecting to Veyron store should be mandatory.
	var store raw.Store
	if storeEndpoint != "" {
		var err error
		store, err = raw.BindStore(naming.JoinAddressName(storeEndpoint, raw.RawStoreSuffix))
		if err != nil {
			vlog.Fatalf("NewSyncd: cannot connect to Veyron store endpoint (%s): %s", storeEndpoint, err)
		}
	}

	return newSyncdCore(peerEndpoints, peerDeviceIDs, devid, storePath, storeEndpoint, store, syncTick)
}

// newSyncdCore is the internal function that creates the Syncd
// structure and initilizes its thread (goroutines).  It takes a
// Veyron Store parameter to separate the core of Syncd setup from the
// external dependency on Veyron Store.
func newSyncdCore(peerEndpoints, peerDeviceIDs, devid, storePath, storeEndpoint string,
	store raw.Store, syncTick time.Duration) *syncd {
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
	s.storeEndpoint = storeEndpoint
	s.store = store
	vlog.VI(1).Infof("newSyncd: Local Veyron store: %s", s.storeEndpoint)

	// Register these Watch data types with VOM.
	// TODO(tilaks): why aren't they auto-retrieved from the IDL?
	vom.Register(&raw.Mutation{})
	vom.Register(&storage.DEntry{})

	// Channel to propagate close event to all threads.
	s.closed = make(chan struct{})

	s.pending.Add(3)

	// Get deltas every peerSyncInterval.
	s.hdlInitiator = newInitiator(s, peerEndpoints, peerDeviceIDs, syncTick)
	go s.hdlInitiator.contactPeers()

	// Garbage collect every garbageCollectInterval.
	s.hdlGC = newGC(s)
	go s.hdlGC.garbageCollect()

	// Start a watcher thread that will get updates from local store.
	s.hdlWatcher = newWatcher(s)
	go s.hdlWatcher.watchStore()

	return s
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

// GetDeltas responds to the incoming request from a client by sending missing generations to the client.
func (s *syncd) GetDeltas(_ ipc.ServerContext, In GenVector, ClientID DeviceID, Stream SyncServiceGetDeltasStream) (GenVector, error) {
	vlog.VI(1).Infof("GetDeltas:: Received vector %v from client %s", In, ClientID)

	// Handle misconfiguration: the client cannot have the same ID as me.
	if ClientID == s.id {
		vlog.VI(1).Infof("GetDeltas:: impostor alert: client ID %s is the same as mine %s", ClientID, s.id)
		return GenVector{}, fmt.Errorf("impostor: you cannot be %s, for I am %s", ClientID, s.id)
	}

	if err := s.updateDeviceInfo(ClientID, In); err != nil {
		vlog.Fatalf("GetDeltas:: updateDeviceInfo failed with err %v", err)
	}

	out, gens, gensInfo, err := s.prepareGensToReply(In)
	if err != nil {
		vlog.Fatalf("GetDeltas:: prepareGensToReply failed with err %v", err)
	}

	for pos, v := range gens {
		gen := gensInfo[pos]
		var count uint64
		for i := LSN(0); i <= gen.MaxLSN; i++ {
			count++
			rec, err := s.getLogRec(v.devID, v.genID, i)
			if err != nil {
				vlog.Fatalf("GetDeltas:: Couldn't get log record %s %d %d, err %v",
					v.devID, v.genID, i, err)
			}
			vlog.VI(1).Infof("Sending log record %v", rec)
			if err := Stream.SendStream().Send(*rec); err != nil {
				vlog.Errorf("GetDeltas:: Couldn't send stream err: %v", err)
				return GenVector{}, err
			}
		}
		if count != gen.Count {
			vlog.Fatalf("GetDeltas:: GenMetadata has incorrect log records for generation %s %d %v",
				v.devID, v.genID, gen)
		}
	}
	return out, nil
}

// updateDeviceInfo updates the remote device's information based on
// the incoming GetDeltas request.
func (s *syncd) updateDeviceInfo(ClientID DeviceID, In GenVector) error {
	s.lock.Lock()
	defer s.lock.Unlock()

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
			return err
		}
	}
	return nil
}

// prepareGensToReply processes the incoming generation vector and
// returns the metadata of all the missing generations between the
// incoming and the local generation vector.
func (s *syncd) prepareGensToReply(In GenVector) (GenVector, []*genOrder, []*genMetadata, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// Get local generation vector.
	out, err := s.devtab.getGenVec(s.id)
	if err != nil {
		return GenVector{}, nil, nil, err
	}

	// Diff the two generation vectors.
	gens, err := s.devtab.diffGenVectors(out, In)
	if err != nil {
		return GenVector{}, nil, nil, err
	}

	// Get the metadata for all the generations in the reply.
	gensInfo := make([]*genMetadata, len(gens))
	for pos, v := range gens {
		gen, err := s.log.getGenMetadata(v.devID, v.genID)
		if err != nil || gen.Count <= 0 {
			return GenVector{}, nil, nil, err
		}
		gensInfo[pos] = gen
	}

	return out, gens, gensInfo, nil
}

// getLogRec gets the log record for a given generation and lsn.
func (s *syncd) getLogRec(dev DeviceID, gen GenID, lsn LSN) (*LogRec, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.log.getLogRec(dev, gen, lsn)
}
