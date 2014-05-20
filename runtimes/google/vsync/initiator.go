package vsync

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"veyron/services/store/raw"

	"veyron2/naming"
	"veyron2/storage"
	"veyron2/vlog"
)

// Policies to pick a peer to sync with.
const (
	// Picks a peer at random from the available set.
	selectRandom = iota

	// TODO(hpucha): implement other policies.
	// Picks a peer with most differing generations.
	selectMostDiff

	// Picks a peer that was synced with the furthest in the past.
	selectOldest
)

// Policies for conflict resolution.
const (
	// Resolves conflicts by picking the mutation with the most recent timestamp.
	useTime = iota

	// TODO(hpucha): implement other policies.
	// Resolves conflicts by using the app conflict resolver callbacks via store.
	useCallback
)

var (
	// peerSyncInterval is the duration between two consecutive
	// sync events.  In every sync event, the initiator contacts
	// one of its peers to obtain any pending updates.
	peerSyncInterval = 100 * time.Millisecond

	// peerSelectionPolicy is the policy used to select a peer when
	// the initiator gets a chance to sync.
	peerSelectionPolicy = selectRandom

	// conflictResolutionPolicy is the policy used to resolve conflicts.
	conflictResolutionPolicy = useTime

	errNoUsefulPeer = errors.New("no useful peer to contact")
)

// syncInitiator contains the metadata and state for the initiator thread.
type syncInitiator struct {
	syncd *syncd

	// State to contact peers periodically and get deltas.
	// TODO(hpucha): This is an initial version with command line arguments.
	// Next steps are to tie this up into mount table and auto-discover neighbors.
	neighbors   []string
	neighborIDs []string
	store       raw.Store

	updObjects map[storage.ID]*objConflictState
}

// objConflictState contains the conflict state for objects that are
// updated during an initiator run.
type objConflictState struct {
	isConflict bool
	newHead    storage.Version
	oldHead    storage.Version
	ancestor   storage.Version
	resolvVal  *LogValue
}

// newInitiator creates a new initiator instance attached to the given syncd instance.
func newInitiator(syncd *syncd, peerEndpoints, peerDeviceIDs string, syncTick time.Duration) *syncInitiator {
	i := &syncInitiator{syncd: syncd,
		updObjects: make(map[storage.ID]*objConflictState),
	}

	// Bootstrap my peer list.
	if peerEndpoints != "" {
		i.neighbors = strings.Split(peerEndpoints, ",")
		i.neighborIDs = strings.Split(peerDeviceIDs, ",")
	}
	if len(i.neighbors) != len(i.neighborIDs) {
		vlog.Fatalf("newInitiator: Mismatch between number of endpoints and IDs")
	}

	// TODO(hpucha): Merge this with vstore handler in syncd.
	if syncd.vstoreEndpoint != "" {
		var err error
		i.store, err = raw.BindStore(naming.JoinAddressName(syncd.vstoreEndpoint, raw.RawStoreSuffix))
		if err != nil {
			vlog.Fatalf("newInitiator: cannot connect to Veyron store endpoint (%s): %s",
				syncd.vstoreEndpoint, err)
		}
	}

	// Override the default peerSyncInterval value if syncTick is specified.
	if syncTick > 0 {
		peerSyncInterval = syncTick
	}

	vlog.VI(1).Infof("newInitiator: My device ID: %s", i.syncd.id)
	vlog.VI(1).Infof("newInitiator: Peer endpoints: %v", i.neighbors)
	vlog.VI(1).Infof("newInitiator: Peer IDs: %v", i.neighborIDs)
	vlog.VI(1).Infof("newInitiator: Sync interval: %v", peerSyncInterval)

	return i
}

// contactPeers wakes up every peerSyncInterval to contact peers and get deltas from them.
func (i *syncInitiator) contactPeers() {
	ticker := time.NewTicker(peerSyncInterval)
	for {
		select {
		case <-i.syncd.closed:
			ticker.Stop()
			i.syncd.pending.Done()
			return
		case <-ticker.C:
		}

		id, ep, err := i.pickPeer()
		if err != nil {
			continue
		}
		i.getDeltasFromPeer(id, ep)
	}
}

// pickPeer picks a sync endpoint in the neighborhood to sync with.
func (i *syncInitiator) pickPeer() (string, string, error) {
	switch peerSelectionPolicy {
	case selectRandom:
		// Pick a neighbor at random.
		if i.neighbors == nil {
			return "", "", errNoUsefulPeer
		}
		ind := rand.Intn(len(i.neighbors))
		return i.neighborIDs[ind], i.neighbors[ind], nil
	default:
		return "", "", fmt.Errorf("unknown peer selection policy")
	}
}

// getDeltasFromPeer contacts the specified endpoint to obtain deltas wrt its current generation vector.
func (i *syncInitiator) getDeltasFromPeer(dID, ep string) {
	vlog.VI(1).Infof("GetDeltasFromPeer:: From server %s with DeviceID %s at %v", ep, dID, time.Now().UTC())

	// Construct a new stub that binds to peer endpoint.
	c, err := BindSync(naming.JoinAddressName(ep, "sync"))
	if err != nil {
		vlog.Errorf("GetDeltasFromPeer:: error binding to server: err %v", err)
		return
	}

	// Get the local generation vector.
	local, err := i.getLocalGenVec()
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error obtaining local gen vector: err %v", err)
	}
	vlog.VI(1).Infof("GetDeltasFromPeer:: Sending local information: %v", local)

	// Issue a GetDeltas() rpc.
	stream, err := c.GetDeltas(local, i.syncd.id)
	if err != nil {
		vlog.Errorf("GetDeltasFromPeer:: error getting deltas: err %v", err)
		return
	}

	minGens, err := i.processLogStream(stream)
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error processing logs: err %v", err)
	}

	remote, err := stream.Finish()
	if err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: finish failed with err %v", err)
	}

	if err := i.processUpdatedObjects(); err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error processing objects: err %v", err)
	}

	if err := i.updateGenVecs(local, minGens, remote, DeviceID(dID)); err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: updateGenVecs failed with err %v", err)
	}

	vlog.VI(1).Infof("GetDeltasFromPeer:: Local vector %v", local)
	vlog.VI(1).Infof("GetDeltasFromPeer:: Remote vector %v", remote)
}

// getLocalGenVec retrieves the local generation vector from the devTable.
func (i *syncInitiator) getLocalGenVec() (GenVector, error) {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.RLock()
	defer i.syncd.lock.RUnlock()

	return i.syncd.devtab.getGenVec(i.syncd.id)
}

// updateGenVecs updates local, reclaim and remote vectors at the end of an initiator cycle.
func (i *syncInitiator) updateGenVecs(local, minGens, remote GenVector, dID DeviceID) error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	// Update the local gen vector and put it in kvdb.
	if err := i.syncd.devtab.updateLocalGenVector(local, remote); err != nil {
		return err
	}

	if err := i.syncd.devtab.putGenVec(i.syncd.id, local); err != nil {
		return err
	}

	if err := i.syncd.devtab.updateReclaimVec(minGens); err != nil {
		return err
	}

	// Cache the remote generation vector for space reclamation.
	if err := i.syncd.devtab.putGenVec(dID, remote); err != nil {
		return err
	}
	return nil
}

// processLogStream replays an entire log stream spanning multiple
// generations from different devices received from a single GetDeltas
// call. It does not perform any conflict resolution during replay.
// This avoids resolving conflicts that have already been resolved by
// other devices.
func (i *syncInitiator) processLogStream(stream SyncGetDeltasStream) (GenVector, error) {
	// Map to track new generations received in the RPC reply.
	// TODO(hpucha): If needed, this can be optimized under the
	// assumption that an entire generation is received
	// sequentially. We can then parse a generation at a time.
	newGens := make(map[string]*genMetadata)
	// Array to track order of arrival for the generations.
	// We need to preserve this order.
	var orderGens []string
	// Compute the minimum generation for every device in this set.
	minGens := GenVector{}

	for {
		rec, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return GenVector{}, err
		}

		if err := i.insertRecInLogAndDag(&rec); err != nil {
			return GenVector{}, err
		}
		// Mark object dirty.
		i.updObjects[rec.ObjID] = &objConflictState{}

		// Populate the generation metadata.
		genKey := generationKey(rec.DevID, rec.GNum)
		if gen, ok := newGens[genKey]; !ok {
			// New generation in the stream.
			orderGens = append(orderGens, genKey)
			newGens[genKey] = &genMetadata{
				Count:  1,
				MaxLSN: rec.LSN,
			}
			g, ok := minGens[rec.DevID]
			if !ok || g > rec.GNum {
				minGens[rec.DevID] = rec.GNum
			}
		} else {
			gen.Count++
			if rec.LSN > gen.MaxLSN {
				gen.MaxLSN = rec.LSN
			}
		}
	}

	if err := i.createGenMetadataBatch(newGens, orderGens); err != nil {
		return GenVector{}, err
	}

	return minGens, nil
}

// insertLogAndDag adds a new log record to log and dag data structures.
func (i *syncInitiator) insertRecInLogAndDag(rec *LogRec) error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	logKey, err := i.syncd.log.putLogRec(rec)
	if err != nil {
		return err
	}
	if err = i.syncd.dag.addNode(rec.ObjID, rec.CurVers, true, rec.Parents, logKey); err != nil {
		return err
	}
	return nil
}

// createGenMetadataBatch inserts a batch of generations into the log.
func (i *syncInitiator) createGenMetadataBatch(newGens map[string]*genMetadata, orderGens []string) error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	for _, key := range orderGens {
		gen := newGens[key]
		// Insert the generation metadata.
		dev, gnum, err := splitGenerationKey(key)
		if err != nil {
			return err
		}
		if err := i.syncd.log.createRemoteGeneration(dev, gnum, gen); err != nil {
			return err
		}
	}

	return nil
}

// processUpdatedObjects processes all the updates received by the
// initiator, one object at a time. For each updated object, we first
// check if the object has any conflicts.  If there is a conflict, we
// resolve the conflict and generate a new store mutation reflecting
// the conflict resolution. If there is no conflict, we generate a
// store mutation to simply update the store to the latest value. We
// then put all these mutations in the store. If the put succeeds, we
// update the log and dag state suitably (move the head ptr of the
// object in the dag to the latest version, and create a new log
// record reflecting conflict resolution if any). Puts to store can
// fail since preconditions on the objects may have been violated. In
// this case, we wait to get the latest versions of objects from the
// store, and recheck if the object has any conflicts and repeat the
// above steps, until put to store succeeds.
func (i *syncInitiator) processUpdatedObjects() error {
	for {
		if err := i.detectConflicts(); err != nil {
			return err
		}

		m, err := i.resolveConflicts()
		if err != nil {
			return err
		}

		err = i.updateStoreAndSync(m)
		if err == nil {
			break
		}

		vlog.Errorf("PutMutations failed %v. Will retry", err)
		// TODO(hpucha): Sleeping and retrying is a temporary
		// solution. Next iteration will have coordination
		// with watch thread to intelligently retry. Hence
		// this value is not a config param.
		time.Sleep(10 * time.Second)
	}

	// Remove any pending state.
	i.updObjects = make(map[storage.ID]*objConflictState)
	i.syncd.dag.clearGraft()
	return nil
}

// detectConflicts iterates through all the updated objects to detect
// conflicts.
func (i *syncInitiator) detectConflicts() error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.RLock()
	defer i.syncd.lock.RUnlock()

	for obj, st := range i.updObjects {
		// Check if object has a conflict.
		var err error
		st.isConflict, st.newHead, st.oldHead, st.ancestor, err = i.syncd.dag.hasConflict(obj)
		if err != nil {
			return err
		}
		if !st.isConflict {
			rec, err := i.getLogRec(obj, st.newHead)
			if err != nil {
				return err
			}
			st.resolvVal = &rec.Value
			// Sanity check.
			if st.resolvVal.Mutation.Version != st.newHead {
				return fmt.Errorf("bad mutation %d %d",
					st.resolvVal.Mutation.Version, st.newHead)
			}
		}
	}
	return nil
}

// getLogRec returns the log record corresponding to a given object and its version.
func (i *syncInitiator) getLogRec(obj storage.ID, vers storage.Version) (*LogRec, error) {
	logKey, err := i.syncd.dag.getLogrec(obj, vers)
	if err != nil {
		return nil, err
	}
	dev, gen, lsn, err := splitLogRecKey(logKey)
	if err != nil {
		return nil, err
	}
	rec, err := i.syncd.log.getLogRec(dev, gen, lsn)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// resolveConflicts resolves conflicts for updated objects.
func (i *syncInitiator) resolveConflicts() ([]raw.Mutation, error) {
	switch conflictResolutionPolicy {
	case useTime:
		if err := i.resolveConflictsByTime(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown conflict resolution policy")
	}

	var m []raw.Mutation
	for _, st := range i.updObjects {
		// Append to mutations.
		st.resolvVal.Mutation.PriorVersion = st.oldHead
		m = append(m, st.resolvVal.Mutation)
	}
	return m, nil
}

// resolveConflictsByTime resolves conflicts using the timestamps
// of the conflicting mutations.  It picks a mutation with the larger
// timestamp, i.e. the most recent update.  If the timestamps are equal,
// it uses the mutation version numbers as a tie-breaker, picking the
// mutation with the larger version.
//
// TODO(hpucha): Based on a few more policies, reconsider nesting
// order of the conflict resolution loop and switch-on-policy.
func (i *syncInitiator) resolveConflictsByTime() error {
	for obj, st := range i.updObjects {
		if !st.isConflict {
			continue
		}

		versions := make([]storage.Version, 3)
		versions[0] = st.oldHead
		versions[1] = st.newHead
		versions[2] = st.ancestor

		lrecs, err := i.getLogRecsBatch(obj, versions)
		if err != nil {
			return err
		}

		res := 0
		switch {
		case lrecs[0].Value.SyncTime > lrecs[1].Value.SyncTime:
			res = 0
		case lrecs[0].Value.SyncTime < lrecs[1].Value.SyncTime:
			res = 1
		case lrecs[0].Value.Mutation.Version > lrecs[1].Value.Mutation.Version:
			res = 0
		case lrecs[0].Value.Mutation.Version < lrecs[1].Value.Mutation.Version:
			res = 1
		}

		m := lrecs[res].Value.Mutation
		m.Version = storage.NewVersion()

		// TODO(hpucha): handle continue and delete flags.
		st.resolvVal = &LogValue{Mutation: m}
	}

	return nil
}

// getLogRecsBatch gets the log records for an array of versions.
func (i *syncInitiator) getLogRecsBatch(obj storage.ID, versions []storage.Version) ([]*LogRec, error) {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.RLock()
	defer i.syncd.lock.RUnlock()

	lrecs := make([]*LogRec, len(versions))
	var err error
	for p, v := range versions {
		lrecs[p], err = i.getLogRec(obj, v)
		if err != nil {
			return nil, err
		}
	}
	return lrecs, nil
}

// updateStoreAndSync updates the store, and if that is successful,
// updates log and dag data structures.
func (i *syncInitiator) updateStoreAndSync(m []raw.Mutation) error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	// TODO(hpucha): PutMutations api is going to change from an
	// array of mutations to one mutation at a time. For now, we
	// will also hold the lock across PutMutations rpc to prevent
	// a race with watcher. The next iteration will clean up this
	// coordination.
	if i.store != nil {
		stream, err := i.store.PutMutations()
		if err != nil {
			return err
		}
		for i := range m {
			if err := stream.Send(m[i]); err != nil {
				return err
			}
		}
		if err := stream.Finish(); err != nil {
			return err
		}
	}

	if err := i.updateLogAndDag(); err != nil {
		return err
	}
	return nil
}

// updateLogAndDag updates the log and dag data structures on a successful store put.
func (i *syncInitiator) updateLogAndDag() error {
	for obj, st := range i.updObjects {
		if st.isConflict {
			// Object had a conflict, which was resolved successfully.
			// Put is successful, create a log record.
			parents := []storage.Version{st.newHead, st.oldHead}
			rec, err := i.syncd.log.createLocalLogRec(obj, st.resolvVal.Mutation.Version, parents, st.resolvVal)
			if err != nil {
				return err
			}

			logKey, err := i.syncd.log.putLogRec(rec)
			if err != nil {
				return err
			}

			// Put is successful, add a new DAG node.
			if err = i.syncd.dag.addNode(obj, st.resolvVal.Mutation.Version, false, parents, logKey); err != nil {
				return err
			}
		}

		// Move the head.
		if err := i.syncd.dag.moveHead(obj, st.resolvVal.Mutation.Version); err != nil {
			return err
		}
	}
	return nil
}
