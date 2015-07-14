// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Initiator is a goroutine that periodically picks a peer from all the known
// remote peers, and requests deltas from that peer for all the SyncGroups in
// common across all apps/databases. It then modifies the sync metadata (DAG and
// local log records) based on the deltas, detects and resolves conflicts if
// any, and suitably updates the local Databases.

import (
	"sort"
	"strings"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/verror"
	"v.io/x/lib/set"
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

var (
	// peerSyncInterval is the duration between two consecutive peer
	// contacts. During every peer contact, the initiator obtains any
	// pending updates from that peer.
	peerSyncInterval = 50 * time.Millisecond

	// peerSelectionPolicy is the policy used to select a peer when
	// the initiator gets a chance to sync.
	peerSelectionPolicy = selectRandom
)

// syncer wakes up every peerSyncInterval to do work: (1) Act as an initiator
// for SyncGroup metadata by selecting a SyncGroup Admin, and syncing Syncgroup
// metadata with it (getting updates from the remote peer, detecting and
// resolving conflicts); (2) Refresh memberView if needed and act as an
// initiator for data by selecting a peer, and syncing data corresponding to all
// common SyncGroups across all Databases; (3) Act as a SyncGroup publisher to
// publish pending SyncGroups; (4) Garbage collect older generations.
//
// TODO(hpucha): Currently only does initiation. Add rest.
func (s *syncService) syncer(ctx *context.T) {
	// TODO(hpucha): Do we need context per initiator round?
	ctx, cancel := context.WithRootCancel(ctx)
	defer cancel()

	ticker := time.NewTicker(peerSyncInterval)
	for {
		select {
		case <-s.closed:
			ticker.Stop()
			s.pending.Done()
			return
		case <-ticker.C:
		}

		// TODO(hpucha): Cut a gen for the responder even if there is no
		// one to initiate to?

		// Do work.
		peer, err := s.pickPeer(ctx)
		if err != nil {
			continue
		}
		s.getDeltasFromPeer(ctx, peer)
	}
}

// getDeltasFromPeer performs an initiation round to the specified
// peer. An initiation round consists of:
// * Contacting the peer to receive all the deltas based on the local genvector.
// * Processing those deltas to discover objects which have been updated.
// * Processing updated objects to detect and resolve any conflicts if needed.
// * Communicating relevant object updates to the Database, and updating local
// genvector to catch up to the received remote genvector.
//
// The processing of the deltas is done one Database at a time. If a local error
// is encountered during the processing of a Database, that Database is skipped
// and the initiator continues on to the next one. If the connection to the peer
// encounters an error, this initiation round is aborted. Note that until the
// local genvector is updated based on the received deltas (the last step in an
// initiation round), the work done by the initiator is idempotent.
//
// TODO(hpucha): Check the idempotence, esp in addNode in DAG.
func (s *syncService) getDeltasFromPeer(ctx *context.T, peer string) {
	info := s.allMembers.members[peer]
	if info == nil {
		vlog.Fatalf("getDeltasFromPeer:: missing information in member view for %q", peer)
	}
	connected := false
	var stream interfaces.SyncGetDeltasClientCall

	// Sync each Database that may have SyncGroups common with this peer,
	// one at a time.
	for gdbName, sgInfo := range info.db2sg {

		// Initialize initiation state for syncing this Database.
		iSt, err := newInitiationState(ctx, s, peer, gdbName, sgInfo)
		if err != nil {
			vlog.Errorf("getDeltasFromPeer:: couldn't initialize initiator state for peer %s, gdb %s, err %v", peer, gdbName, err)
			continue
		}

		if len(iSt.sgIds) == 0 || len(iSt.sgPfxs) == 0 {
			vlog.Errorf("getDeltasFromPeer:: didn't find any SyncGroups for peer %s, gdb %s, err %v", peer, gdbName, err)
			continue
		}

		// Make contact with the peer once.
		if !connected {
			stream, connected = iSt.connectToPeer(ctx)
			if !connected {
				// Try a different Database. Perhaps there are
				// different mount tables.
				continue
			}
		}

		// Create local genvec so that it contains knowledge only about common prefixes.
		if err := iSt.createLocalGenVec(ctx); err != nil {
			vlog.Errorf("getDeltasFromPeer:: error creating local genvec for gdb %s, err %v", gdbName, err)
			continue
		}

		iSt.stream = stream
		req := interfaces.DeltaReq{
			AppName: iSt.appName,
			DbName:  iSt.dbName,
			SgIds:   iSt.sgIds,
			InitVec: iSt.local,
		}

		sender := iSt.stream.SendStream()
		sender.Send(req)

		// Obtain deltas from the peer over the network.
		if err := iSt.recvAndProcessDeltas(ctx); err != nil {
			vlog.Errorf("getDeltasFromPeer:: error receiving deltas for gdb %s, err %v", gdbName, err)
			// Returning here since something could be wrong with
			// the connection, and no point in attempting the next
			// Database.
			return
		}

		if err := iSt.processUpdatedObjects(ctx); err != nil {
			vlog.Errorf("getDeltasFromPeer:: error processing objects for gdb %s, err %v", gdbName, err)
			// Move to the next Database even if processing updates
			// failed.
			continue
		}
	}

	if connected {
		stream.Finish()
	}
}

// initiationState is accumulated for each Database during an initiation round.
type initiationState struct {
	// Relative name of the peer to sync with.
	peer string

	// Collection of mount tables that this peer may have registered with.
	mtTables map[string]struct{}

	// SyncGroups being requested in the initiation round.
	sgIds map[interfaces.GroupId]struct{}

	// SyncGroup prefixes being requested in the initiation round.
	sgPfxs map[string]struct{}

	// Local generation vector.
	local interfaces.GenVector

	// Generation vector from the remote peer.
	remote interfaces.GenVector

	// Updated local generation vector at the end of the initiation round.
	updLocal interfaces.GenVector

	// State to track updated objects during a log replay.
	updObjects map[string]*objConflictState

	// DAG state that tracks conflicts and common ancestors.
	dagGraft graftMap

	sync    *syncService
	appName string
	dbName  string
	st      store.Store                        // Store handle to the Database.
	stream  interfaces.SyncGetDeltasClientCall // Stream handle for the GetDeltas RPC.

	// Transaction handle for the initiation round. Used during the update
	// of objects in the Database.
	tx store.Transaction
}

// objConflictState contains the conflict state for an object that is updated
// during an initiator round.
type objConflictState struct {
	isConflict bool
	newHead    string
	oldHead    string
	ancestor   string
	res        *conflictResolution
}

// newInitiationState creates new initiation state.
func newInitiationState(ctx *context.T, s *syncService, peer string, name string, sgInfo sgMemberInfo) (*initiationState, error) {
	iSt := &initiationState{}
	iSt.peer = peer
	iSt.updObjects = make(map[string]*objConflictState)
	iSt.dagGraft = newGraft()
	iSt.sync = s

	// TODO(hpucha): Would be nice to standardize on the combined "app:db"
	// name across sync (not syncbase) so we only join split/join them at
	// the boundary with the store part.
	var err error
	iSt.appName, iSt.dbName, err = splitAppDbName(ctx, name)
	if err != nil {
		return nil, err
	}

	// TODO(hpucha): nil rpc.ServerCall ok?
	iSt.st, err = s.getDbStore(ctx, nil, iSt.appName, iSt.dbName)
	if err != nil {
		return nil, err
	}

	iSt.peerMtTblsAndSgInfo(ctx, peer, sgInfo)

	return iSt, nil
}

// peerMtTblsAndSgInfo computes the possible mount tables, the SyncGroup Ids and
// prefixes common with a remote peer in a particular Database by consulting the
// SyncGroups in the specified Database.
func (iSt *initiationState) peerMtTblsAndSgInfo(ctx *context.T, peer string, info sgMemberInfo) {
	iSt.mtTables = make(map[string]struct{})
	iSt.sgIds = make(map[interfaces.GroupId]struct{})
	iSt.sgPfxs = make(map[string]struct{})

	for id := range info {
		sg, err := getSyncGroupById(ctx, iSt.st, id)
		if err != nil {
			continue
		}
		if _, ok := sg.Joiners[peer]; !ok {
			// Peer is no longer part of the SyncGroup.
			continue
		}
		for _, mt := range sg.Spec.MountTables {
			iSt.mtTables[mt] = struct{}{}
		}
		iSt.sgIds[id] = struct{}{}

		for _, p := range sg.Spec.Prefixes {
			iSt.sgPfxs[p] = struct{}{}
		}
	}
}

// connectToPeer attempts to connect to the remote peer using the mount tables
// obtained from the SyncGroups being synced in the current Database.
func (iSt *initiationState) connectToPeer(ctx *context.T) (interfaces.SyncGetDeltasClientCall, bool) {
	if len(iSt.mtTables) < 1 {
		vlog.Errorf("getDeltasFromPeer:: no mount tables found to connect to peer %s, app %s db %s", iSt.peer, iSt.appName, iSt.dbName)
		return nil, false
	}
	for mt := range iSt.mtTables {
		absName := naming.Join(mt, iSt.peer, util.SyncbaseSuffix)
		c := interfaces.SyncClient(absName)
		stream, err := c.GetDeltas(ctx)
		if err == nil {
			return stream, true
		}
	}
	return nil, false
}

// createLocalGenVec creates the generation vector with local knowledge for the
// initiator to send to the responder.
//
// TODO(hpucha): Refactor this code with computeDelta code in sync_state.go.
func (iSt *initiationState) createLocalGenVec(ctx *context.T) error {
	// Freeze the most recent batch of local changes before fetching
	// remote changes from a peer. This frozen state is used by the
	// responder when responding to GetDeltas RPC.
	//
	// We only allow an initiator to freeze local generations (not
	// responders/watcher) in order to maintain a static baseline
	// for the duration of a sync. This addresses the following race
	// condition: If we allow responders to use newer local
	// generations while the initiator is in progress, they may beat
	// the initiator and send these new generations to remote
	// devices.  These remote devices in turn can send these
	// generations back to the initiator in progress which was
	// started with older generation information.
	if err := iSt.sync.checkptLocalGen(ctx, iSt.appName, iSt.dbName); err != nil {
		return err
	}

	local, lgen, err := iSt.sync.copyDbGenInfo(ctx, iSt.appName, iSt.dbName)
	if err != nil {
		return err
	}
	localPfxs := extractAndSortPrefixes(local)

	sgPfxs := set.String.ToSlice(iSt.sgPfxs)
	sort.Strings(sgPfxs)

	iSt.local = make(interfaces.GenVector)

	if len(sgPfxs) == 0 {
		return verror.New(verror.ErrInternal, ctx, "no syncgroups for syncing")
	}

	pfx := sgPfxs[0]
	for _, p := range sgPfxs {
		if strings.HasPrefix(p, pfx) && p != pfx {
			continue
		}

		// Process this prefix as this is the start of a new set of
		// nested prefixes.
		pfx = p
		var lpStart string
		for _, lp := range localPfxs {
			if !strings.HasPrefix(lp, pfx) && !strings.HasPrefix(pfx, lp) {
				// No relationship with pfx.
				continue
			}
			if strings.HasPrefix(pfx, lp) {
				lpStart = lp
			} else {
				iSt.local[lp] = local[lp]
			}
		}
		// Deal with the starting point.
		if lpStart == "" {
			// No matching prefixes for pfx were found.
			iSt.local[pfx] = make(interfaces.PrefixGenVector)
			iSt.local[pfx][iSt.sync.id] = lgen
		} else {
			iSt.local[pfx] = local[lpStart]
		}
	}
	return nil
}

// recvAndProcessDeltas first receives the log records and generation vector
// from the GetDeltas RPC and puts them in the Database. It also replays the
// entire log stream as the log records arrive. These records span multiple
// generations from different devices. It does not perform any conflict
// resolution during replay.  This avoids resolving conflicts that have already
// been resolved by other devices.
func (iSt *initiationState) recvAndProcessDeltas(ctx *context.T) error {
	// TODO(hpucha): This works for now, but figure out a long term solution
	// as this may be implementation dependent. It currently works because
	// the RecvStream call is stateless, and grabbing a handle to it
	// repeatedly doesn't affect what data is seen next.
	rcvr := iSt.stream.RecvStream()
	start, finish := false, false

	// TODO(hpucha): See if we can avoid committing the entire delta stream
	// as one batch. Currently the dependency is between the log records and
	// the batch info.
	tx := iSt.st.NewTransaction()
	committed := false

	defer func() {
		if !committed {
			tx.Abort()
		}
	}()

	// Track received batches (BatchId --> BatchCount mapping).
	batchMap := make(map[uint64]uint64)

	for rcvr.Advance() {
		resp := rcvr.Value()
		switch v := resp.(type) {
		case interfaces.DeltaRespStart:
			if start {
				return verror.New(verror.ErrInternal, ctx, "received start followed by start in delta response stream")
			}
			start = true

		case interfaces.DeltaRespFinish:
			if finish {
				return verror.New(verror.ErrInternal, ctx, "received finish followed by finish in delta response stream")
			}
			finish = true

		case interfaces.DeltaRespRespVec:
			iSt.remote = v.Value

		case interfaces.DeltaRespRec:
			// Insert log record in Database.
			// TODO(hpucha): Should we reserve more positions in a batch?
			// TODO(hpucha): Handle if SyncGroup is left/destroyed while sync is in progress.
			pos := iSt.sync.reservePosInDbLog(ctx, iSt.appName, iSt.dbName, 1)
			rec := &localLogRec{Metadata: v.Value.Metadata, Pos: pos}
			batchId := rec.Metadata.BatchId
			if batchId != NoBatchId {
				if cnt, ok := batchMap[batchId]; !ok {
					if iSt.sync.startBatch(ctx, tx, batchId) != batchId {
						return verror.New(verror.ErrInternal, ctx, "failed to create batch info")
					}
					batchMap[batchId] = rec.Metadata.BatchCount
				} else if cnt != rec.Metadata.BatchCount {
					return verror.New(verror.ErrInternal, ctx, "inconsistent counts for tid", batchId, cnt, rec.Metadata.BatchCount)
				}
			}

			if err := iSt.insertRecInLogDagAndDb(ctx, rec, batchId, v.Value.Value, tx); err != nil {
				return err
			}
			// Mark object dirty.
			iSt.updObjects[rec.Metadata.ObjId] = &objConflictState{}
		}

		// Break out of the stream.
		if finish {
			break
		}
	}

	if !(start && finish) {
		return verror.New(verror.ErrInternal, ctx, "didn't receive start/finish delimiters in delta response stream")
	}

	if err := rcvr.Err(); err != nil {
		return err
	}

	// End the started batches if any.
	for bid, cnt := range batchMap {
		if err := iSt.sync.endBatch(ctx, tx, bid, cnt); err != nil {
			return err
		}
	}

	// Commit this transaction. We do not retry this transaction since it
	// should not conflict with any other keys. So if it fails, it is a
	// non-retriable error.
	err := tx.Commit()
	if verror.ErrorID(err) == store.ErrConcurrentTransaction.ID {
		// Note: This might be triggered with memstore until it handles
		// transactions in a more fine-grained fashion.
		vlog.Fatalf("recvAndProcessDeltas:: encountered concurrent transaction")
	}
	if err == nil {
		committed = true
	}
	return err
}

// insertRecInLogDagAndDb adds a new log record to log and dag data structures,
// and inserts the versioned value in the Database.
func (iSt *initiationState) insertRecInLogDagAndDb(ctx *context.T, rec *localLogRec, batchId uint64, valbuf []byte, tx store.Transaction) error {
	if err := putLogRec(ctx, tx, rec); err != nil {
		return err
	}

	m := rec.Metadata
	logKey := logRecKey(m.Id, m.Gen)

	var err error
	switch m.RecType {
	case interfaces.NodeRec:
		err = iSt.sync.addNode(ctx, tx, m.ObjId, m.CurVers, logKey, m.Delete, m.Parents, m.BatchId, iSt.dagGraft)
	case interfaces.LinkRec:
		err = iSt.sync.addParent(ctx, tx, m.ObjId, m.CurVers, m.Parents[0], iSt.dagGraft)
	default:
		err = verror.New(verror.ErrInternal, ctx, "unknown log record type")
	}

	if err != nil {
		return err
	}
	// TODO(hpucha): Hack right now. Need to change Database's handling of
	// deleted objects. Currently, the initiator needs to treat deletions
	// specially since deletions do not get a version number or a special
	// value in the Database.
	if !rec.Metadata.Delete && rec.Metadata.RecType == interfaces.NodeRec {
		return watchable.PutAtVersion(ctx, tx, []byte(m.ObjId), valbuf, []byte(m.CurVers))
	}
	return nil
}

// processUpdatedObjects processes all the updates received by the initiator,
// one object at a time. Conflict detection and resolution is carried out after
// the entire delta of log records is replayed, instead of incrementally after
// each record/batch is replayed, to avoid repeating conflict resolution already
// performed by other peers.
//
// For each updated object, we first check if the object has any conflicts,
// resulting in three possibilities:
//
// * There is no conflict, and no updates are needed to the Database
// (isConflict=false, newHead == oldHead). All changes received convey
// information that still keeps the local head as the most recent version. This
// occurs when conflicts are resolved by picking the existing local version.
//
// * There is no conflict, but a remote version is discovered that builds on the
// local head (isConflict=false, newHead != oldHead). In this case, we generate
// a Database update to simply update the Database to the latest value.
//
// * There is a conflict and we call into the app or use a well-known policy to
// resolve the conflict, resulting in three possibilties: (a) conflict was
// resolved by picking the local version. In this case, Database need not be
// updated, but a link is added to record the choice. (b) conflict was resolved
// by picking the remote version. In this case, Database is updated with the
// remote version and a link is added as well. (c) conflict was resolved by
// generating a new Database update. In this case, Database is updated with the
// new version.
//
// We collect all the updates to the Database in a transaction. In addition, as
// part of the same transaction, we update the log and dag state suitably (move
// the head ptr of the object in the dag to the latest version, and create a new
// log record reflecting conflict resolution if any). Finally, we update the
// sync state first on storage. This transaction's commit can fail since
// preconditions on the objects may have been violated. In this case, we wait to
// get the latest versions of objects from the Database, and recheck if the object
// has any conflicts and repeat the above steps, until the transaction commits
// successfully. Upon commit, we also update the in-memory sync state of the
// Database.
func (iSt *initiationState) processUpdatedObjects(ctx *context.T) error {
	// Note that the tx handle in initiation state is cached for the scope of
	// this function only as different stages in the pipeline add to the
	// transaction.
	committed := false
	defer func() {
		if !committed {
			iSt.tx.Abort()
		}
	}()

	for {
		iSt.tx = iSt.st.NewTransaction()
		watchable.SetTransactionFromSync(iSt.tx) // for echo-suppression

		if err := iSt.detectConflicts(ctx); err != nil {
			return err
		}

		if err := iSt.resolveConflicts(ctx); err != nil {
			return err
		}

		if err := iSt.updateDbAndSyncSt(ctx); err != nil {
			return err
		}

		err := iSt.tx.Commit()
		if err == nil {
			committed = true
			// Update in-memory genvector since commit is successful.
			if err := iSt.sync.putDbGenInfoRemote(ctx, iSt.appName, iSt.dbName, iSt.updLocal); err != nil {
				vlog.Fatalf("processUpdatedObjects:: putting geninfo in memory failed for app %s db %s, err %v", iSt.appName, iSt.dbName, err)
			}
			return nil
		}

		if verror.ErrorID(err) != store.ErrConcurrentTransaction.ID {
			return err
		}

		// TODO(hpucha): Sleeping and retrying is a temporary
		// solution. Next iteration will have coordination with watch
		// thread to intelligently retry. Hence this value is not a
		// config param.
		iSt.tx.Abort()
		time.Sleep(1 * time.Second)
	}
}

// detectConflicts iterates through all the updated objects to detect conflicts.
func (iSt *initiationState) detectConflicts(ctx *context.T) error {
	for objid, confSt := range iSt.updObjects {
		// Check if object has a conflict.
		var err error
		confSt.isConflict, confSt.newHead, confSt.oldHead, confSt.ancestor, err = hasConflict(ctx, iSt.tx, objid, iSt.dagGraft)
		if err != nil {
			return err
		}

		if !confSt.isConflict {
			confSt.res = &conflictResolution{ty: pickRemote}
		}
	}
	return nil
}

// updateDbAndSync updates the Database, and if that is successful, updates log,
// dag and genvector data structures as needed.
func (iSt *initiationState) updateDbAndSyncSt(ctx *context.T) error {
	for objid, confSt := range iSt.updObjects {
		// If the local version is picked, no further updates to the
		// Database are needed.
		if confSt.res.ty == pickLocal {
			continue
		}

		// If the remote version is picked or if a new version is
		// created, we put it in the Database.

		// TODO(hpucha): Hack right now. Need to change Database's
		// handling of deleted objects.
		oldVersDeleted := true
		if confSt.oldHead != NoVersion {
			oldDagNode, err := getNode(ctx, iSt.tx, objid, confSt.oldHead)
			if err != nil {
				return err
			}
			oldVersDeleted = oldDagNode.Deleted
		}

		var newVersion string
		var newVersDeleted bool
		switch confSt.res.ty {
		case pickRemote:
			newVersion = confSt.newHead
			newDagNode, err := getNode(ctx, iSt.tx, objid, newVersion)
			if err != nil {
				return err
			}
			newVersDeleted = newDagNode.Deleted
		case createNew:
			newVersion = confSt.res.rec.Metadata.CurVers
			newVersDeleted = confSt.res.rec.Metadata.Delete
		}

		// Skip delete followed by a delete.
		if oldVersDeleted && newVersDeleted {
			continue
		}

		if !oldVersDeleted {
			// Read current version to enter it in the readset of the transaction.
			version, err := watchable.GetVersion(ctx, iSt.tx, []byte(objid))
			if err != nil {
				return err
			}
			if string(version) != confSt.oldHead {
				return store.NewErrConcurrentTransaction(ctx)
			}
		} else {
			// Ensure key doesn't exist.
			if _, err := watchable.GetVersion(ctx, iSt.tx, []byte(objid)); verror.ErrorID(err) != store.ErrUnknownKey.ID {
				return store.NewErrConcurrentTransaction(ctx)
			}
		}

		if !newVersDeleted {
			if confSt.res.ty == createNew {
				if err := watchable.PutAtVersion(ctx, iSt.tx, []byte(objid), confSt.res.val, []byte(newVersion)); err != nil {
					return err
				}
			}
			if err := watchable.PutVersion(ctx, iSt.tx, []byte(objid), []byte(newVersion)); err != nil {
				return err
			}
		} else {
			if err := iSt.tx.Delete([]byte(objid)); err != nil {
				return err
			}
		}

		if err := iSt.updateLogAndDag(ctx, objid); err != nil {
			return err
		}
	}

	return iSt.updateSyncSt(ctx)
}

// updateLogAndDag updates the log and dag data structures.
func (iSt *initiationState) updateLogAndDag(ctx *context.T, obj string) error {
	confSt, ok := iSt.updObjects[obj]
	if !ok {
		return verror.New(verror.ErrInternal, ctx, "object state not found", obj)
	}
	var newVersion string

	if !confSt.isConflict {
		newVersion = confSt.newHead
	} else {
		// Object had a conflict. Create a log record to reflect resolution.
		var rec *localLogRec

		switch {
		case confSt.res.ty == pickLocal:
			// Local version was picked as the conflict resolution.
			rec = iSt.createLocalLinkLogRec(ctx, obj, confSt.oldHead, confSt.newHead)
			newVersion = confSt.oldHead
		case confSt.res.ty == pickRemote:
			// Remote version was picked as the conflict resolution.
			rec = iSt.createLocalLinkLogRec(ctx, obj, confSt.newHead, confSt.oldHead)
			newVersion = confSt.newHead
		default:
			// New version was created to resolve the conflict.
			rec = confSt.res.rec
			newVersion = confSt.res.rec.Metadata.CurVers
		}

		if err := putLogRec(ctx, iSt.tx, rec); err != nil {
			return err
		}

		// Add a new DAG node.
		var err error
		m := rec.Metadata
		switch m.RecType {
		case interfaces.NodeRec:
			err = iSt.sync.addNode(ctx, iSt.tx, obj, m.CurVers, logRecKey(m.Id, m.Gen), m.Delete, m.Parents, NoBatchId, nil)
		case interfaces.LinkRec:
			err = iSt.sync.addParent(ctx, iSt.tx, obj, m.CurVers, m.Parents[0], nil)
		default:
			return verror.New(verror.ErrInternal, ctx, "unknown log record type")
		}
		if err != nil {
			return err
		}
	}

	// Move the head. This should be idempotent. We may move head to the
	// local head in some cases.
	return moveHead(ctx, iSt.tx, obj, newVersion)
}

func (iSt *initiationState) createLocalLinkLogRec(ctx *context.T, obj, vers, par string) *localLogRec {
	gen, pos := iSt.sync.reserveGenAndPosInDbLog(ctx, iSt.appName, iSt.dbName, 1)

	rec := &localLogRec{
		Metadata: interfaces.LogRecMetadata{
			Id:      iSt.sync.id,
			Gen:     gen,
			RecType: interfaces.LinkRec,

			ObjId:      obj,
			CurVers:    vers,
			Parents:    []string{par},
			UpdTime:    time.Now().UTC(),
			BatchId:    NoBatchId,
			BatchCount: 1,
			// TODO(hpucha): What is its batchid and count?
		},
		Pos: pos,
	}
	return rec
}

// updateSyncSt updates local sync state at the end of an initiator cycle.
func (iSt *initiationState) updateSyncSt(ctx *context.T) error {
	// Get the current local sync state.
	dsInMem, err := iSt.sync.copyDbSyncStateInMem(ctx, iSt.appName, iSt.dbName)
	if err != nil {
		return err
	}
	ds := &dbSyncState{
		Gen:        dsInMem.gen,
		CheckptGen: dsInMem.checkptGen,
		GenVec:     dsInMem.genvec,
	}

	// remote can be a subset of local.
	for rpfx, respgv := range iSt.remote {
		for lpfx, lpgv := range ds.GenVec {
			if strings.HasPrefix(lpfx, rpfx) {
				mergePrefixGenVectors(lpgv, respgv)
			}
		}
		if _, ok := ds.GenVec[rpfx]; !ok {
			ds.GenVec[rpfx] = respgv
		}
	}

	iSt.updLocal = ds.GenVec
	// Clean the genvector of any local state. Note that local state is held
	// in gen/ckPtGen in sync state struct.
	for _, pgv := range iSt.updLocal {
		delete(pgv, iSt.sync.id)
	}

	// TODO(hpucha): Add knowledge compaction.

	return putDbSyncState(ctx, iSt.tx, ds)
}

// mergePrefixGenVectors merges responder prefix genvector into local genvector.
func mergePrefixGenVectors(lpgv, respgv interfaces.PrefixGenVector) {
	for devid, rgen := range respgv {
		gen, ok := lpgv[devid]
		if !ok || gen < rgen {
			lpgv[devid] = rgen
		}
	}
}

////////////////////////////////////////
// Peer selection policies.

// pickPeer picks a Syncbase to sync with.
func (s *syncService) pickPeer(ctx *context.T) (string, error) {
	switch peerSelectionPolicy {
	case selectRandom:
		members := s.getMembers(ctx)
		// Remove myself from the set.
		delete(members, s.name)
		if len(members) == 0 {
			return "", verror.New(verror.ErrInternal, ctx, "no useful peer")
		}

		// Pick a peer at random.
		ind := randIntn(len(members))
		for m := range members {
			if ind == 0 {
				return m, nil
			}
			ind--
		}
		return "", verror.New(verror.ErrInternal, ctx, "random selection didn't succeed")
	default:
		return "", verror.New(verror.ErrInternal, ctx, "unknown peer selection policy")
	}
}
