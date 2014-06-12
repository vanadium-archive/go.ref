package vsync

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"veyron/services/store/raw"

	"veyron2/context"
	"veyron2/naming"
	"veyron2/rt"
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
	peerSyncInterval = 50 * time.Millisecond

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
	if peerEndpoints != "" || peerDeviceIDs != "" {
		i.neighbors = strings.Split(peerEndpoints, ",")
		i.neighborIDs = strings.Split(peerDeviceIDs, ",")
		if len(i.neighbors) != len(i.neighborIDs) {
			vlog.Fatalf("newInitiator: Mismatch between number of endpoints and IDs")
		}

		// Neighbor IDs must be distinct and different from my ID.
		neighborIDs := make(map[string]struct{})
		for _, nID := range i.neighborIDs {
			if DeviceID(nID) == i.syncd.id {
				vlog.Fatalf("newInitiator: neighboor ID %v cannot be the same as my ID %v", nID, i.syncd.id)
			}
			if _, ok := neighborIDs[nID]; ok {
				vlog.Fatalf("newInitiator: neighboor ID %v is duplicated", nID)
			}
			neighborIDs[nID] = struct{}{}
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

		// Freeze the most recent batch of local changes
		// before fetching remote changes from a peer.
		//
		// We only allow an initiator to create new local
		// generations (not responders/watcher) in order to
		// maintain a static baseline for the duration of a
		// sync. This addresses the following race condition:
		// If we allow responders to create new local
		// generations while the initiator is in progress,
		// they may beat the initiator and send these new
		// generations to remote devices.  These remote
		// devices in turn can send these generations back to
		// the initiator in progress which was started with
		// older generation information.
		local, err := i.updateLocalGeneration()
		if err != nil {
			vlog.Fatalf("contactPeers:: error updating local generation: err %v", err)
		}

		i.getDeltasFromPeer(id, ep, local)
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

// updateLocalGeneration creates a new local generation if needed and
// returns the newest local generation vector.
func (i *syncInitiator) updateLocalGeneration() (GenVector, error) {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	// Create a new local generation if there are any local updates.
	gen, err := i.syncd.log.createLocalGeneration()
	if err == errNoUpdates {
		vlog.VI(1).Infof("createLocalGeneration:: No new updates. Local at %d", gen)
		return i.syncd.devtab.getGenVec(i.syncd.id)
	}
	if err != nil {
		return GenVector{}, err
	}

	vlog.VI(2).Infof("updateLocalGeneration:: created gen %d", gen)
	// Update local generation vector in devTable.
	if err = i.syncd.devtab.updateGeneration(i.syncd.id, i.syncd.id, gen); err != nil {
		return GenVector{}, err
	}
	return i.syncd.devtab.getGenVec(i.syncd.id)
}

// getDeltasFromPeer contacts the specified endpoint to obtain deltas wrt its current generation vector.
func (i *syncInitiator) getDeltasFromPeer(dID, ep string, local GenVector) {
	ctx := rt.R().NewContext()

	vlog.VI(1).Infof("GetDeltasFromPeer:: From server %s with DeviceID %s at %v", ep, dID, time.Now().UTC())

	// Construct a new stub that binds to peer endpoint.
	c, err := BindSync(naming.JoinAddressName(ep, "sync"))
	if err != nil {
		vlog.Errorf("GetDeltasFromPeer:: error binding to server: err %v", err)
		return
	}

	vlog.VI(1).Infof("GetDeltasFromPeer:: Sending local information: %v", local)

	// Issue a GetDeltas() rpc.
	stream, err := c.GetDeltas(ctx, local, i.syncd.id)
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

	if err := i.processUpdatedObjects(ctx, local, minGens, remote, DeviceID(dID)); err != nil {
		vlog.Fatalf("GetDeltasFromPeer:: error processing objects: err %v", err)
	}

	vlog.VI(1).Infof("GetDeltasFromPeer:: Local vector %v", local)
	vlog.VI(1).Infof("GetDeltasFromPeer:: Remote vector %v", remote)
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

	vlog.VI(2).Infof("insertRecInLogAndDag:: Adding log record %v", rec)
	switch rec.RecType {
	case NodeRec:
		return i.syncd.dag.addNode(rec.ObjID, rec.CurVers, true, rec.Parents, logKey)
	case LinkRec:
		return i.syncd.dag.addParent(rec.ObjID, rec.CurVers, rec.Parents[0], true)
	default:
		return fmt.Errorf("unknown log record type")
	}
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
// check if the object has any conflicts, resulting in three
// possibilities:
//
// * There is no conflict, and no updates are needed to the store
// (isConflict=false, newHead == oldHead). All changes received convey
// information that still keeps the local head as the most recent
// version. This occurs when conflicts are resolved by blessings.
//
// * There is no conflict, but a remote version is discovered that
// builds on the local head (isConflict=false, newHead != oldHead). In
// this case, we generate a store mutation to simply update the store
// to the latest value.
//
// * There is a conflict and we call into the app or the system to
// resolve the conflict, resulting in three possibilties: (a) conflict
// was resolved by blessing the local version. In this case, store
// need not be updated, but a link is added to record the
// blessing. (b) conflict was resolved by blessing the remote
// version. In this case, store is updated with the remote version and
// a link is added as well. (c) conflict was resolved by generating a
// new store mutation. In this case, store is updated with the new
// version.
//
// We then put all these mutations in the store. If the put succeeds,
// we update the log and dag state suitably (move the head ptr of the
// object in the dag to the latest version, and create a new log
// record reflecting conflict resolution if any). Puts to store can
// fail since preconditions on the objects may have been violated. In
// this case, we wait to get the latest versions of objects from the
// store, and recheck if the object has any conflicts and repeat the
// above steps, until put to store succeeds.
func (i *syncInitiator) processUpdatedObjects(ctx context.T, local, minGens, remote GenVector, dID DeviceID) error {
	for {
		if err := i.detectConflicts(); err != nil {
			return err
		}

		if err := i.resolveConflicts(); err != nil {
			return err
		}

		err := i.updateStoreAndSync(ctx, local, minGens, remote, dID)
		if err == nil {
			break
		}

		vlog.Errorf("PutMutations failed %v. Will retry", err)
		// TODO(hpucha): Sleeping and retrying is a temporary
		// solution. Next iteration will have coordination
		// with watch thread to intelligently retry. Hence
		// this value is not a config param.
		time.Sleep(1 * time.Second)
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
		vlog.VI(2).Infof("detectConflicts:: object %v state %v err %v",
			obj, st, err)
		if err != nil {
			return err
		}
	}
	return nil
}

// resolveConflicts resolves conflicts for updated objects. Conflicts
// may be resolved by adding new versions or blessing either the local
// or the remote version.
func (i *syncInitiator) resolveConflicts() error {
	switch conflictResolutionPolicy {
	case useTime:
		if err := i.resolveConflictsByTime(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown conflict resolution policy")
	}
	return nil
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

		// Instead of creating a new version that resolves the
		// conflict, we are blessing an existing version as
		// the conflict resolution.
		st.resolvVal = &lrecs[res].Value
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
func (i *syncInitiator) updateStoreAndSync(ctx context.T, local, minGens, remote GenVector, dID DeviceID) error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	var m []raw.Mutation
	for obj, st := range i.updObjects {
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

		// If the local version is picked, no further updates
		// to the store are needed. If the remote version is
		// picked, we put it in the store.
		if st.resolvVal.Mutation.Version != st.oldHead {
			// Append to mutations.
			st.resolvVal.Mutation.PriorVersion = st.oldHead
			vlog.VI(2).Infof("updateStoreAndSync:: appending mutation %v for obj %v",
				st.resolvVal.Mutation, obj)
			m = append(m, st.resolvVal.Mutation)
		}
	}

	// TODO(hpucha): We will hold the lock across PutMutations rpc
	// to prevent a race with watcher. The next iteration will
	// clean up this coordination.
	if store := i.syncd.store; store != nil && len(m) > 0 {
		stream, err := store.PutMutations(ctx)
		if err != nil {
			vlog.Errorf("updateStoreAndSync:: putmutations err %v", err)
			return err
		}
		for i := range m {
			if err := stream.Send(m[i]); err != nil {
				vlog.Errorf("updateStoreAndSync:: send err %v", err)
				return err
			}
		}
		if err := stream.CloseSend(); err != nil {
			vlog.Errorf("updateStoreAndSync:: closesend err %v", err)
			return err
		}
		if err := stream.Finish(); err != nil {
			vlog.Errorf("updateStoreAndSync:: finish err %v", err)
			return err
		}
	}

	vlog.VI(2).Infof("updateStoreAndSync:: putmutations succeeded")
	if err := i.updateLogAndDag(); err != nil {
		return err
	}

	if err := i.updateGenVecs(local, minGens, remote, DeviceID(dID)); err != nil {
		return err
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

// updateLogAndDag updates the log and dag data structures on a successful store put.
func (i *syncInitiator) updateLogAndDag() error {
	for obj, st := range i.updObjects {
		if st.isConflict {
			// Object had a conflict, which was resolved successfully.
			// Put is successful, create a log record.
			var err error
			var rec *LogRec

			switch {
			case st.resolvVal.Mutation.Version == st.oldHead:
				// Local version was blessed as the conflict resolution.
				rec, err = i.syncd.log.createLocalLinkLogRec(obj, st.oldHead, st.newHead)
			case st.resolvVal.Mutation.Version == st.newHead:
				// Remote version was blessed as the conflict resolution.
				rec, err = i.syncd.log.createLocalLinkLogRec(obj, st.newHead, st.oldHead)
			default:
				// New version was created to resolve the conflict.
				parents := []storage.Version{st.newHead, st.oldHead}
				rec, err = i.syncd.log.createLocalLogRec(obj, st.resolvVal.Mutation.Version, parents, st.resolvVal)

			}
			if err != nil {
				return err
			}
			logKey, err := i.syncd.log.putLogRec(rec)
			if err != nil {
				return err
			}
			// Add a new DAG node.
			switch rec.RecType {
			case NodeRec:
				err = i.syncd.dag.addNode(obj, rec.CurVers, false, rec.Parents, logKey)
			case LinkRec:
				err = i.syncd.dag.addParent(obj, rec.CurVers, rec.Parents[0], false)
			default:
				return fmt.Errorf("unknown log record type")
			}
			if err != nil {
				return err
			}
		}

		// Move the head. This should be idempotent. We may
		// move head to the local head in some cases.
		if err := i.syncd.dag.moveHead(obj, st.resolvVal.Mutation.Version); err != nil {
			return err
		}
	}
	return nil
}

// updateGenVecs updates local, reclaim and remote vectors at the end of an initiator cycle.
func (i *syncInitiator) updateGenVecs(local, minGens, remote GenVector, dID DeviceID) error {
	// Update the local gen vector and put it in kvdb only if we have new updates.
	if len(i.updObjects) > 0 {
		if err := i.syncd.devtab.updateLocalGenVector(local, remote); err != nil {
			return err
		}

		if err := i.syncd.devtab.putGenVec(i.syncd.id, local); err != nil {
			return err
		}

		if err := i.syncd.devtab.updateReclaimVec(minGens); err != nil {
			return err
		}
	}

	// Cache the remote generation vector for space reclamation.
	if err := i.syncd.devtab.putGenVec(dID, remote); err != nil {
		return err
	}
	return nil
}
