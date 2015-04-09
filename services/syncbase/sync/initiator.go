// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
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

	// State accumulated during an initiation round.
	iState *initiationState
}

// initiationState accumulated during an initiation round.
type initiationState struct {

	// Veyron name of peer being synced with.
	peer string

	// Local generation vector.
	local map[ObjId]GenVector

	// SyncGroups being requested in the initiation round.
	syncGroups map[ObjId]GroupIdSet

	// Map to track new generations received in the RPC reply.
	newGens map[string]*genMetadata

	// Array to track order of arrival for the generations.
	// We need to preserve this order for the replay.
	orderGens []string

	// Generation vector to track the oldest generation received
	// in the RPC reply per device, for garbage collection.
	minGens map[ObjId]GenVector

	// Generation vector from the remote peer.
	remote map[ObjId]GenVector

	// Tmp kvdb state.
	tmpFile string
	tmpDB   *kvdb
	tmpTbl  *kvtable

	// State to track updated objects during a log replay.
	updObjects map[ObjId]*objConflictState

	// State to delete objects from the "priv" table.
	delPrivObjs map[ObjId]struct{}
}

// objConflictState contains the conflict state for objects that are
// updated during an initiator run.
type objConflictState struct {
	isConflict bool
	newHead    Version
	oldHead    Version
	ancestor   Version
	resolvVal  *LogValue
	srID       ObjId
}

// newInitiator creates a new initiator instance attached to the given syncd instance.
func newInitiator(syncd *syncd, syncTick time.Duration) *syncInitiator {
	i := &syncInitiator{syncd: syncd}

	// Override the default peerSyncInterval value if syncTick is specified.
	if syncTick > 0 {
		peerSyncInterval = syncTick
	}

	vlog.VI(1).Infof("newInitiator: My device ID: %s", i.syncd.id)
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

		peerRelName, err := i.pickPeer()
		if err != nil {
			continue
		}

		i.getDeltasFromPeer(peerRelName)
	}
}

// pickPeer picks a sync endpoint to sync with.
func (i *syncInitiator) pickPeer() (string, error) {
	myID := string(i.syncd.relName)

	switch peerSelectionPolicy {
	case selectRandom:
		// TODO(hpucha): Eliminate reaching into syncd's lock.
		i.syncd.lock.RLock()
		// neighbors, err := i.syncd.sgtab.getMembers()
		neighbors := make(map[string]uint32)
		var err error
		i.syncd.lock.RUnlock()

		if err != nil {
			return "", err
		}

		// Remove myself from the set so only neighbors are counted.
		delete(neighbors, myID)

		if len(neighbors) == 0 {
			return "", errNoUsefulPeer
		}

		// Pick a neighbor at random.
		ind := rand.Intn(len(neighbors))
		for k := range neighbors {
			if ind == 0 {
				return k, nil
			}
			ind--
		}
		return "", fmt.Errorf("random selection didn't succeed")
	default:
		return "", fmt.Errorf("unknown peer selection policy")
	}
}

// getDeltasFromPeer performs an initiation round to the specified
// peer. An initiation round consists of:
// * Creating local generation for syncroots that are going to be requested in this round.
// * Contacting the peer to receive all the deltas based on the local gen vector.
// * Processing those deltas to discover objects which have been updated.
// * Processing updated objects to detect and resolve any conflicts if needed.
// * Communicating relevant object updates to the store.
func (i *syncInitiator) getDeltasFromPeer(peerRelName string) {

	vlog.VI(2).Infof("getDeltasFromPeer:: %s", peerRelName)
	// Initialize initiation state.
	i.newInitiationState()
	defer i.clearInitiationState()

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
	err := i.updateLocalGeneration(peerRelName)
	if err != nil {
		vlog.Fatalf("getDeltasFromPeer:: error updating local generation: err %v", err)
	}

	// Obtain deltas from the peer over the network. These deltas
	// are stored in a tmp kvdb.
	if err := i.getDeltas(); err != nil {
		vlog.Errorf("getDeltasFromPeer:: error getting deltas: err %v", err)
		return
	}

	i.syncd.sgOp.Lock()
	defer i.syncd.sgOp.Unlock()

	if err := i.processDeltas(); err != nil {
		vlog.Fatalf("getDeltasFromPeer:: error processing logs: err %v", err)
	}

	if err := i.processUpdatedObjects(); err != nil {
		vlog.Fatalf("getDeltasFromPeer:: error processing objects: err %v", err)
	}
}

// newInitiationState creates new initiation state.
func (i *syncInitiator) newInitiationState() {
	st := &initiationState{}
	st.local = make(map[ObjId]GenVector)
	st.syncGroups = make(map[ObjId]GroupIdSet)
	st.newGens = make(map[string]*genMetadata)
	st.minGens = make(map[ObjId]GenVector)
	st.remote = make(map[ObjId]GenVector)
	st.updObjects = make(map[ObjId]*objConflictState)
	st.delPrivObjs = make(map[ObjId]struct{})

	i.iState = st
}

// clearInitiationState cleans up the state from the current initiation round.
func (i *syncInitiator) clearInitiationState() {
	if i.iState.tmpDB != nil {
		i.iState.tmpDB.close()
	}
	if i.iState.tmpFile != "" {
		os.Remove(i.iState.tmpFile)
	}

	for o := range i.iState.delPrivObjs {
		i.syncd.dag.delPrivNode(o)
	}

	i.syncd.dag.clearGraft()

	i.iState = nil
}

// updateLocalGeneration creates a new local generation if needed and
// populates the newest local generation vector.
func (i *syncInitiator) updateLocalGeneration(peerRelName string) error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	// peerInfo, err := i.syncd.sgtab.getMemberInfo(peerRelName)
	// if err != nil {
	// 	return err
	// }

	// Re-construct all mount table possibilities.
	mtTables := make(map[string]struct{})

	// for sg := range peerInfo.gids {
	// 	sgData, err := i.syncd.sgtab.getSyncGroupByID(sg)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	sr := ObjId(sgData.SrvInfo.RootObjId)
	// 	for _, mt := range sgData.SrvInfo.Config.MountTables {
	// 		mtTables[mt] = struct{}{}
	// 	}
	// 	i.iState.syncGroups[sr] = append(i.iState.syncGroups[sr], sg)
	// }

	// Create a new local generation if there are any local updates
	// for every syncroot that is common with the "peer" to be
	// contacted.
	for sr := range i.iState.syncGroups {
		gen, err := i.syncd.log.createLocalGeneration(sr)
		if err != nil && err != errNoUpdates {
			return err
		}

		if err == nil {
			vlog.VI(2).Infof("updateLocalGeneration:: created gen for sr %s at %d", sr.String(), gen)

			// Update local generation vector in devTable.
			if err = i.syncd.devtab.updateGeneration(i.syncd.id, sr, i.syncd.id, gen); err != nil {
				return err
			}
		}

		v, err := i.syncd.devtab.getGenVec(i.syncd.id, sr)
		if err != nil {
			return err
		}

		i.iState.local[sr] = v
	}

	// Check if the name is absolute, and if so, use the name as-is.
	if naming.Rooted(peerRelName) {
		i.iState.peer = peerRelName
		return nil
	}

	// Pick any global name to contact the peer.
	for mt := range mtTables {
		i.iState.peer = naming.Join(mt, peerRelName)
		vlog.VI(2).Infof("updateLocalGeneration:: contacting peer %s", i.iState.peer)
		return nil
	}

	return fmt.Errorf("no mounttables found")
}

// getDeltas obtains the deltas from the peer and stores them in a tmp kvdb.
func (i *syncInitiator) getDeltas() error {
	// As log records are streamed, they are saved
	// in a tmp kvdb so that they can be processed in one batch.
	if err := i.initTmpKVDB(); err != nil {
		return err
	}

	ctx, _ := vtrace.SetNewTrace(i.syncd.ctx)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	vlog.VI(1).Infof("getDeltas:: From peer with DeviceId %s at %v", i.iState.peer, time.Now().UTC())

	// Construct a new stub that binds to peer endpoint.
	c := SyncClient(naming.JoinAddressName(i.iState.peer, SyncSuffix))

	vlog.VI(1).Infof("GetDeltasFromPeer:: Sending local information: %v", i.iState.local)

	// Issue a GetDeltas() rpc.
	stream, err := c.GetDeltas(ctx, i.iState.local, i.iState.syncGroups, i.syncd.id)
	if err != nil {
		return err
	}

	return i.recvLogStream(stream)
}

// initTmpKVDB initializes the tmp kvdb to store the log record stream.
func (i *syncInitiator) initTmpKVDB() error {
	i.iState.tmpFile = fmt.Sprintf("%s/tmp_%d", i.syncd.kvdbPath, time.Now().UnixNano())
	tmpDB, tbls, err := kvdbOpen(i.iState.tmpFile, []string{"tmprec"})
	if err != nil {
		return err
	}
	i.iState.tmpDB = tmpDB
	i.iState.tmpTbl = tbls[0]
	return nil
}

// recvLogStream receives the log records from the GetDeltas stream
// and puts them in tmp kvdb for later processing.
func (i *syncInitiator) recvLogStream(stream SyncGetDeltasClientCall) error {
	rStream := stream.RecvStream()
	for rStream.Advance() {
		rec := rStream.Value()

		// Insert log record in tmpTbl.
		if err := i.iState.tmpTbl.set(logRecKey(rec.DevId, rec.SyncRootId, rec.GenNum, rec.SeqNum), &rec); err != nil {
			// TODO(hpucha): do we need to cancel stream?
			return err
		}

		// Populate the generation metadata.
		genKey := generationKey(rec.DevId, rec.SyncRootId, rec.GenNum)
		if gen, ok := i.iState.newGens[genKey]; !ok {
			// New generation in the stream.
			i.iState.orderGens = append(i.iState.orderGens, genKey)
			i.iState.newGens[genKey] = &genMetadata{
				Count:     1,
				MaxSeqNum: rec.SeqNum,
			}
			if _, ok := i.iState.minGens[rec.SyncRootId]; !ok {
				i.iState.minGens[rec.SyncRootId] = GenVector{}
			}
			g, ok := i.iState.minGens[rec.SyncRootId][rec.DevId]
			if !ok || g > rec.GenNum {
				i.iState.minGens[rec.SyncRootId][rec.DevId] = rec.GenNum
			}
		} else {
			gen.Count++
			if rec.SeqNum > gen.MaxSeqNum {
				gen.MaxSeqNum = rec.SeqNum
			}
		}
	}

	if err := rStream.Err(); err != nil {
		return err
	}

	var err error
	if i.iState.remote, err = stream.Finish(); err != nil {
		return err
	}
	vlog.VI(1).Infof("recvLogStream:: Local vector %v", i.iState.local)
	vlog.VI(1).Infof("recvLogStream:: Remote vector %v", i.iState.remote)
	vlog.VI(2).Infof("recvLogStream:: orderGens %v", i.iState.orderGens)
	return nil
}

// processDeltas replays an entire log stream spanning multiple
// generations from different devices received from a single GetDeltas
// call. It does not perform any conflict resolution during replay.
// This avoids resolving conflicts that have already been resolved by
// other devices.
func (i *syncInitiator) processDeltas() error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	// Track received transactions.
	txMap := make(map[TxId]uint32)

	// Loop through each received generation in order.
	for _, key := range i.iState.orderGens {
		gen := i.iState.newGens[key]
		dev, sr, gnum, err := splitGenerationKey(key)
		if err != nil {
			return err
		}

		// If "sr" has been left since getDeltas, skip processing.
		// if !i.syncd.sgtab.isSyncRoot(sr) {
		// 	continue
		// }

		for l := SeqNum(0); l <= gen.MaxSeqNum; l++ {
			var rec LogRec
			if err := i.iState.tmpTbl.get(logRecKey(dev, sr, gnum, l), &rec); err != nil {
				return err
			}

			// Begin a new transaction if needed.
			curTx := rec.Value.TxId
			if curTx != NoTxId {
				if cnt, ok := txMap[curTx]; !ok {
					if i.syncd.dag.addNodeTxStart(curTx) != curTx {
						return fmt.Errorf("failed to create transaction")
					}
					txMap[curTx] = rec.Value.TxCount
					vlog.VI(2).Infof("processDeltas:: Begin Tx %v, count %d", curTx, rec.Value.TxCount)
				} else if cnt != rec.Value.TxCount {
					return fmt.Errorf("inconsistent counts for tid %v %d, %d", curTx, cnt, rec.Value.TxCount)
				}
			}

			if err := i.insertRecInLogAndDag(&rec, curTx); err != nil {
				return err
			}

			// Mark object dirty.
			i.iState.updObjects[rec.ObjId] = &objConflictState{
				srID: rec.SyncRootId,
			}
		}
		// Insert the generation metadata.
		if err := i.syncd.log.createRemoteGeneration(dev, sr, gnum, gen); err != nil {
			return err
		}
	}

	// End the started transactions if any.
	for t, cnt := range txMap {
		if err := i.syncd.dag.addNodeTxEnd(t, cnt); err != nil {
			return err
		}
		vlog.VI(2).Infof("processLogStream:: End Tx %v %v", t, cnt)
	}

	return nil
}

// insertRecInLogAndDag adds a new log record to log and dag data structures.
func (i *syncInitiator) insertRecInLogAndDag(rec *LogRec, txID TxId) error {
	logKey, err := i.syncd.log.putLogRec(rec)
	if err != nil {
		return err
	}

	vlog.VI(2).Infof("insertRecInLogAndDag:: Adding log record %v, Tx %v", rec, txID)
	switch rec.RecType {
	case NodeRec:
		return i.syncd.dag.addNode(rec.ObjId, rec.CurVers, true, rec.Value.Delete, rec.Parents, logKey, txID)
	case LinkRec:
		return i.syncd.dag.addParent(rec.ObjId, rec.CurVers, rec.Parents[0], true)
	default:
		return fmt.Errorf("unknown log record type")
	}
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
func (i *syncInitiator) processUpdatedObjects() error {
	for {
		if err := i.detectConflicts(); err != nil {
			return err
		}

		if err := i.resolveConflicts(); err != nil {
			return err
		}

		err := i.updateStoreAndSync()
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

	return nil
}

// detectConflicts iterates through all the updated objects to detect
// conflicts.
func (i *syncInitiator) detectConflicts() error {
	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.RLock()
	defer i.syncd.lock.RUnlock()

	for obj, st := range i.iState.updObjects {
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
	for obj, st := range i.iState.updObjects {
		if !st.isConflict {
			continue
		}

		res, err := i.resolveObjConflict(obj, st.oldHead, st.newHead, st.ancestor)
		if err != nil {
			return err
		}

		st.resolvVal = res
	}

	return nil
}

// resolveObjConflict resolves a conflict for an object given its ID and
// the 3 versions that express the conflict: the object's local version, its
// remote version (from the device contacted), and the common ancestor from
// which both versions branched away.  The function returns the new object
// value according to the conflict resolution policy.
func (i *syncInitiator) resolveObjConflict(oid ObjId,
	local, remote, ancestor Version) (*LogValue, error) {

	// Fetch the log records of the 3 object versions.
	versions := []Version{local, remote, ancestor}
	lrecs, err := i.getLogRecsBatch(oid, versions)
	if err != nil {
		return nil, err
	}

	// Resolve the conflict according to the resolution policy.
	var res *LogValue

	switch conflictResolutionPolicy {
	case useTime:
		res, err = i.resolveObjConflictByTime(oid, lrecs[0], lrecs[1], lrecs[2])
	default:
		err = fmt.Errorf("unknown conflict resolution policy: %v", conflictResolutionPolicy)
	}

	if err != nil {
		return nil, err
	}

	resCopy := *res
	// resCopy.Mutation.Version = NewVersion()
	// resCopy.Mutation.Dir = resDir
	resCopy.SyncTime = time.Now().UnixNano()
	resCopy.TxId = NoTxId
	resCopy.TxCount = 0
	return &resCopy, nil
}

// resolveObjConflictByTime resolves conflicts using the timestamps
// of the conflicting mutations.  It picks a mutation with the larger
// timestamp, i.e. the most recent update.  If the timestamps are equal,
// it uses the mutation version numbers as a tie-breaker, picking the
// mutation with the larger version.
// Instead of creating a new version that resolves the conflict, we are
// blessing an existing version as the conflict resolution.
func (i *syncInitiator) resolveObjConflictByTime(oid ObjId,
	local, remote, ancestor *LogRec) (*LogValue, error) {

	var res *LogValue
	switch {
	case local.Value.SyncTime > remote.Value.SyncTime:
		res = &local.Value
	case local.Value.SyncTime < remote.Value.SyncTime:
		res = &remote.Value
		// case local.Value.Mutation.Version > remote.Value.Mutation.Version:
		// 	res = &local.Value
		// case local.Value.Mutation.Version < remote.Value.Mutation.Version:
		// 	res = &remote.Value
	}

	return res, nil
}

// getLogRecsBatch gets the log records for an array of versions.
func (i *syncInitiator) getLogRecsBatch(obj ObjId, versions []Version) ([]*LogRec, error) {
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
func (i *syncInitiator) updateStoreAndSync() error {

	// TODO(hpucha): Eliminate reaching into syncd's lock.
	i.syncd.lock.Lock()
	defer i.syncd.lock.Unlock()

	// var m []raw.Mutation
	// for obj, st := range i.iState.updObjects {
	// 	if !st.isConflict {
	// 		rec, err := i.getLogRec(obj, st.newHead)
	// 		if err != nil {
	// 			return err
	// 		}
	// 		st.resolvVal = &rec.Value
	// 		// Sanity check.
	// 		if st.resolvVal.Mutation.Version != st.newHead {
	// 			return fmt.Errorf("bad mutation %d %d",
	// 				st.resolvVal.Mutation.Version, st.newHead)
	// 		}
	// 	}

	// 	// If the local version is picked, no further updates
	// 	// to the store are needed. If the remote version is
	// 	// picked, we put it in the store.
	// 	if st.resolvVal.Mutation.Version != st.oldHead {
	// 		st.resolvVal.Mutation.PriorVersion = st.oldHead

	// 		// Convert resolvVal.Mutation into a mutation for the Store.
	// 		stMutation, err := i.storeMutation(obj, st.resolvVal)
	// 		if err != nil {
	// 			return err
	// 		}

	// 		vlog.VI(2).Infof("updateStoreAndSync:: Try to append mutation %v (%v) for obj %v (nh %v, oh %v)",
	// 			st.resolvVal.Mutation, stMutation, obj, st.newHead, st.oldHead)

	// 		// Append to mutations, skipping a delete following a delete mutation.
	// 		if stMutation.Version != NoVersion ||
	// 			stMutation.PriorVersion != NoVersion {
	// 			vlog.VI(2).Infof("updateStoreAndSync:: appending mutation %v for obj %v",
	// 				stMutation, obj)
	// 			m = append(m, stMutation)
	// 		}
	// 	}
	// }

	// TODO(hpucha): We will hold the lock across PutMutations rpc
	// to prevent a race with watcher. The next iteration will
	// clean up this coordination.
	// if store := i.syncd.store; store != nil && len(m) > 0 {
	// 	ctx, _ := vtrace.SetNewTrace(i.syncd.ctx)
	// 	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	// 	defer cancel()

	// 	stream, err := store.PutMutations(ctx)
	// 	if err != nil {
	// 		vlog.Errorf("updateStoreAndSync:: putmutations err %v", err)
	// 		return err
	// 	}
	// 	sender := stream.SendStream()
	// 	for i := range m {
	// 		if err := sender.Send(m[i]); err != nil {
	// 			vlog.Errorf("updateStoreAndSync:: send err %v", err)
	// 			return err
	// 		}
	// 	}
	// 	if err := sender.Close(); err != nil {
	// 		vlog.Errorf("updateStoreAndSync:: closesend err %v", err)
	// 		return err
	// 	}
	// 	if err := stream.Finish(); err != nil {
	// 		vlog.Errorf("updateStoreAndSync:: finish err %v", err)
	// 		return err
	// 	}
	// }

	vlog.VI(2).Infof("updateStoreAndSync:: putmutations succeeded")
	if err := i.updateLogAndDag(); err != nil {
		return err
	}

	if err := i.updateGenVecs(); err != nil {
		return err
	}

	return nil
}

// storeMutation converts a resolved mutation generated by syncd to
// one that can be sent to the store. To send to the store, it
// converts the version numbers corresponding to object deletions to
// NoVersion when required. It also converts the version number
// appropriately to handle SyncGroup join.
// func (i *syncInitiator) storeMutation(obj ObjId, resolvVal *LogValue) (raw.Mutation, error) {
// 	curDelete := resolvVal.Delete
// 	priorDelete := false
// 	if resolvVal.Mutation.PriorVersion != raw.NoVersion {
// 		oldRec, err := i.getLogRec(obj, resolvVal.Mutation.PriorVersion)
// 		if err != nil {
// 			return raw.Mutation{}, err
// 		}
// 		priorDelete = oldRec.Value.Delete
// 	}

// 	// Handle the versioning of a SyncGroup's root ObjId during join.
// 	if resolvVal.Mutation.PriorVersion == raw.NoVersion {
// 		if i.syncd.sgtab.isSyncRoot(obj) {
// 			node, err := i.syncd.dag.getPrivNode(obj)
// 			if err != nil {
// 				return raw.Mutation{}, err
// 			}
// 			resolvVal.Mutation.PriorVersion = node.Mutation.Version
// 			i.iState.delPrivObjs[obj] = struct{}{}
// 		}
// 	}

// 	// Current version and prior versions are not deletes.
// 	if !curDelete && !priorDelete {
// 		return resolvVal.Mutation, nil
// 	}

// 	// Creating a new copy of the mutation to adjust version
// 	// numbers when handling deletions.
// 	stMutation := resolvVal.Mutation
// 	// Adjust the current version if this a deletion.
// 	if curDelete {
// 		stMutation.Version = NoVersion
// 	}
// 	// Adjust the prior version if it is a deletion.
// 	if priorDelete {
// 		stMutation.PriorVersion = NoVersion
// 	}

// 	return stMutation, nil
// }

// getLogRec returns the log record corresponding to a given object and its version.
func (i *syncInitiator) getLogRec(obj ObjId, vers Version) (*LogRec, error) {
	logKey, err := i.syncd.dag.getLogrec(obj, vers)
	vlog.VI(2).Infof("getLogRec:: logkey from dag is %s", logKey)
	if err != nil {
		return nil, err
	}
	dev, sg, gen, lsn, err := splitLogRecKey(logKey)
	if err != nil {
		return nil, err
	}
	vlog.VI(2).Infof("getLogRec:: splitting logkey %s to %s %v %v %v", logKey, dev, sg, gen, lsn)
	rec, err := i.syncd.log.getLogRec(dev, sg, gen, lsn)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// updateLogAndDag updates the log and dag data structures on a successful store put.
func (i *syncInitiator) updateLogAndDag() error {
	for obj, st := range i.iState.updObjects {
		if st.isConflict {
			// Object had a conflict, which was resolved successfully.
			// Put is successful, create a log record.
			var err error
			var rec *LogRec

			// switch {
			// case st.resolvVal.Mutation.Version == st.oldHead:
			// 	// Local version was blessed as the conflict resolution.
			// 	rec, err = i.syncd.log.createLocalLinkLogRec(obj, st.oldHead, st.newHead, st.srID)
			// case st.resolvVal.Mutation.Version == st.newHead:
			// 	// Remote version was blessed as the conflict resolution.
			// 	rec, err = i.syncd.log.createLocalLinkLogRec(obj, st.newHead, st.oldHead, st.srID)
			// default:
			// 	// New version was created to resolve the conflict.
			// 	parents := []Version{st.newHead, st.oldHead}
			// 	rec, err = i.syncd.log.createLocalLogRec(obj, st.resolvVal.Mutation.Version, parents, st.resolvVal, st.srID)
			// }
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
				// TODO(hpucha): addNode operations arising out of conflict resolution
				// may need to be part of a transaction when app-driven resolution
				// is introduced.
				err = i.syncd.dag.addNode(obj, rec.CurVers, false, rec.Value.Delete, rec.Parents, logKey, NoTxId)
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
		// if err := i.syncd.dag.moveHead(obj, st.resolvVal.Mutation.Version); err != nil {
		// 	return err
		// }
	}
	return nil
}

// updateGenVecs updates local, reclaim and remote vectors at the end of an initiator cycle.
func (i *syncInitiator) updateGenVecs() error {
	// Update the local gen vector and put it in kvdb only if we have new updates.
	if len(i.iState.updObjects) > 0 {
		// remote can be a subset of local.
		for sr, rVec := range i.iState.remote {
			lVec := i.iState.local[sr]

			if err := i.syncd.devtab.updateLocalGenVector(lVec, rVec); err != nil {
				return err
			}

			if err := i.syncd.devtab.putGenVec(i.syncd.id, sr, lVec); err != nil {
				return err
			}

			// if err := i.syncd.devtab.updateReclaimVec(minGens); err != nil {
			// 	return err
			// }
		}
	}

	for sr, rVec := range i.iState.remote {
		// Cache the remote generation vector for space reclamation.
		if err := i.syncd.devtab.putGenVec(i.syncd.nameToDevId(i.iState.peer), sr, rVec); err != nil {
			return err
		}
	}
	return nil
}
