// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Initiator requests deltas from a chosen peer for all the SyncGroups in common
// across all apps/databases. It then modifies the sync metadata (DAG and local
// log records) based on the deltas, detects and resolves conflicts if any, and
// suitably updates the local Databases.

import (
	"sort"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/syncbase/nosql"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/set"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
)

// getDeltas performs an initiation round to the specified peer. An
// initiation round consists of two sync rounds:
// * Sync SyncGroup metadata.
// * Sync data.
// Each sync round involves:
// * Contacting the peer to receive all the deltas based on the local genvector.
// * Processing those deltas to discover objects which have been updated.
// * Processing updated objects to detect and resolve any conflicts if needed.
// * Communicating relevant object updates to the Database in case of data.
// * Updating local genvector to catch up to the received remote genvector.
//
// The processing of the deltas is done one Database at a time, encompassing all
// the SyncGroups common to the initiator and the responder. If a local error is
// encountered during the processing of a Database, that Database is skipped and
// the initiator continues on to the next one. If the connection to the peer
// encounters an error, this initiation round is aborted. Note that until the
// local genvector is updated based on the received deltas (the last step in an
// initiation round), the work done by the initiator is idempotent.
//
// TODO(hpucha): Check the idempotence, esp in addNode in DAG.
func (s *syncService) getDeltas(ctx *context.T, peer string) {
	vlog.VI(2).Infof("sync: getDeltas: begin: contacting peer %s", peer)
	defer vlog.VI(2).Infof("sync: getDeltas: end: contacting peer %s", peer)

	info := s.copyMemberInfo(ctx, peer)
	if info == nil {
		vlog.Fatalf("sync: getDeltas: missing information in member view for %q", peer)
	}

	// Preferred mount tables for this peer.
	prfMtTbls := set.String.ToSlice(info.mtTables)

	// Sync each Database that may have SyncGroups common with this peer,
	// one at a time.
	for gdbName := range info.db2sg {
		vlog.VI(4).Infof("sync: getDeltas: started for peer %s db %s", peer, gdbName)

		if len(prfMtTbls) < 1 {
			vlog.Errorf("sync: getDeltas: no mount tables found to connect to peer %s", peer)
			return
		}

		c, err := newInitiationConfig(ctx, s, peer, gdbName, info, prfMtTbls)
		if err != nil {
			vlog.Errorf("sync: getDeltas: couldn't initialize initiator config for peer %s, gdb %s, err %v", peer, gdbName, err)
			continue
		}

		if err := s.getDBDeltas(ctx, peer, c, true); err == nil {
			if err := s.getDBDeltas(ctx, peer, c, false); err != nil {
				vlog.Errorf("sync: getDeltas: failed for data sync, err %v", err)
			}
		} else {
			// If SyncGroup sync fails, abort data sync as well.
			vlog.Errorf("sync: getDeltas: failed for SyncGroup sync, err %v", err)
		}

		// Cache the pruned mount table list for the next Database.
		prfMtTbls = c.mtTables

		vlog.VI(4).Infof("sync: getDeltas: done for peer %s db %s", peer, gdbName)
	}
}

// getDBDeltas gets the deltas from the chosen peer. If sg flag is set to true,
// it will sync SyncGroup metadata. If sg flag is false, it will sync data.
func (s *syncService) getDBDeltas(ctxIn *context.T, peer string, c *initiationConfig, sg bool) error {
	vlog.VI(2).Infof("sync: getDBDeltas: begin: contacting peer sg %v %s", sg, peer)
	defer vlog.VI(2).Infof("sync: getDBDeltas: end: contacting peer sg %v %s", sg, peer)

	ctx, cancel := context.WithRootCancel(ctxIn)
	// cancel() is idempotent.
	defer cancel()

	// Initialize initiation state for syncing this Database.
	iSt := newInitiationState(ctx, c, sg)

	// Initialize SyncGroup prefixes for data syncing.
	if !sg {
		iSt.peerSgInfo(ctx)
		if len(iSt.config.sgPfxs) == 0 {
			return verror.New(verror.ErrInternal, ctx, "no SyncGroup prefixes found", peer, iSt.config.appName, iSt.config.dbName)
		}
	}

	if sg {
		// Create local genvec so that it contains knowledge about
		// common SyncGroups and then send the SyncGroup metadata sync
		// request.
		if err := iSt.prepareSGDeltaReq(ctx); err != nil {
			return err
		}
	} else {
		// Create local genvec so that it contains knowledge only about common
		// prefixes and then send the data sync request.
		if err := iSt.prepareDataDeltaReq(ctx); err != nil {
			return err
		}
	}

	// Make contact with the peer.
	if !iSt.connectToPeer(ctx) {
		return verror.New(verror.ErrInternal, ctx, "couldn't connect to peer", peer)
	}

	// Obtain deltas from the peer over the network.
	if err := iSt.recvAndProcessDeltas(ctx); err != nil {
		cancel()
		iSt.stream.Finish()
		return err
	}

	vlog.VI(4).Infof("sync: getDBDeltas: got reply: %v", iSt.remote)

	if err := iSt.processUpdatedObjects(ctx); err != nil {
		return err
	}

	return iSt.stream.Finish()
}

type sgSet map[interfaces.GroupId]struct{}

// initiationConfig is the configuration information for a Database in an
// initiation round.
type initiationConfig struct {
	peer string // relative name of the peer to sync with.

	// Mount tables that this peer may have registered with. The first entry
	// in this array is the mount table where the peer was successfully
	// reached the last time.
	mtTables []string

	sgIds   sgSet            // SyncGroups being requested in the initiation round.
	sgPfxs  map[string]sgSet // SyncGroup prefixes and their ids being requested in the initiation round.
	sync    *syncService
	appName string
	dbName  string
	st      store.Store // Store handle to the Database.
}

// initiationState is accumulated for a Database in each sync round in an
// initiation round.
type initiationState struct {
	// Config information.
	config *initiationConfig

	// Accumulated sync state.
	local      interfaces.GenVector         // local generation vector.
	remote     interfaces.GenVector         // generation vector from the remote peer.
	updLocal   interfaces.GenVector         // updated local generation vector at the end of sync round.
	updObjects map[string]*objConflictState // tracks updated objects during a log replay.
	dagGraft   *graftMap                    // DAG state that tracks conflicts and common ancestors.

	req    interfaces.DeltaReq                // GetDeltas RPC request.
	stream interfaces.SyncGetDeltasClientCall // stream handle for the GetDeltas RPC.

	// Flag to indicate if this is SyncGroup metadata sync.
	sg bool

	// Transaction handle for the sync round. Used during the update
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

// newInitiatonConfig creates new initiation config. This will be shared between
// the two sync rounds in the initiation round of a Database.
func newInitiationConfig(ctx *context.T, s *syncService, peer string, name string, info *memberInfo, mtTables []string) (*initiationConfig, error) {
	c := &initiationConfig{}
	c.peer = peer
	c.mtTables = mtTables
	c.sgIds = make(sgSet)
	for id := range info.db2sg[name] {
		c.sgIds[id] = struct{}{}
	}
	if len(c.sgIds) == 0 {
		return nil, verror.New(verror.ErrInternal, ctx, "no SyncGroups found", peer, name)
	}
	// Note: sgPfxs will be inited when needed by the data sync.

	c.sync = s

	// TODO(hpucha): Would be nice to standardize on the combined "app:db"
	// name across sync (not syncbase) so we only join split/join them at
	// the boundary with the store part.
	var err error
	c.appName, c.dbName, err = splitAppDbName(ctx, name)
	if err != nil {
		return nil, err
	}

	// TODO(hpucha): nil rpc.ServerCall ok?
	c.st, err = s.getDbStore(ctx, nil, c.appName, c.dbName)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// newInitiationState creates new initiation state.
func newInitiationState(ctx *context.T, c *initiationConfig, sg bool) *initiationState {
	iSt := &initiationState{}
	iSt.config = c
	iSt.updObjects = make(map[string]*objConflictState)
	iSt.dagGraft = newGraft(c.st)
	iSt.sg = sg
	return iSt
}

// peerSgInfo computes the SyncGroup Ids and prefixes common with a remote peer
// in a particular Database by consulting the SyncGroups in the specified
// Database.
func (iSt *initiationState) peerSgInfo(ctx *context.T) {
	sgs := iSt.config.sgIds
	iSt.config.sgIds = make(sgSet) // regenerate the sgids since we are going through the SyncGroups in any case.
	iSt.config.sgPfxs = make(map[string]sgSet)

	for id := range sgs {
		sg, err := getSyncGroupById(ctx, iSt.config.st, id)
		if err != nil {
			continue
		}
		if _, ok := sg.Joiners[iSt.config.peer]; !ok {
			// Peer is no longer part of the SyncGroup.
			continue
		}

		iSt.config.sgIds[id] = struct{}{}

		for _, p := range sg.Spec.Prefixes {
			sgs, ok := iSt.config.sgPfxs[p]
			if !ok {
				sgs = make(sgSet)
				iSt.config.sgPfxs[p] = sgs
			}
			sgs[id] = struct{}{}
		}
	}
}

// prepareDataDeltaReq creates the generation vector with local knowledge for the
// initiator to send to the responder, and creates the request to start the data
// sync.
//
// TODO(hpucha): Refactor this code with computeDelta code in sync_state.go.
func (iSt *initiationState) prepareDataDeltaReq(ctx *context.T) error {
	iSt.config.sync.thLock.Lock()
	defer iSt.config.sync.thLock.Unlock()

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
	if err := iSt.config.sync.checkptLocalGen(ctx, iSt.config.appName, iSt.config.dbName, nil); err != nil {
		return err
	}

	local, lgen, err := iSt.config.sync.copyDbGenInfo(ctx, iSt.config.appName, iSt.config.dbName, nil)
	if err != nil {
		return err
	}

	localPfxs := extractAndSortPrefixes(local)

	sgPfxs := make([]string, len(iSt.config.sgPfxs))
	i := 0
	for p := range iSt.config.sgPfxs {
		sgPfxs[i] = p
		i++
	}
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
			iSt.local[pfx][iSt.config.sync.id] = lgen
		} else {
			iSt.local[pfx] = local[lpStart]
		}
	}

	// Send request.
	req := interfaces.DataDeltaReq{
		AppName: iSt.config.appName,
		DbName:  iSt.config.dbName,
		SgIds:   iSt.config.sgIds,
		InitVec: iSt.local,
	}

	iSt.req = interfaces.DeltaReqData{req}

	vlog.VI(4).Infof("sync: prepareDataDeltaReq: request: %v", req)

	return nil
}

// prepareSGDeltaReq creates the SyncGroup generation vector with local knowledge
// for the initiator to send to the responder, and prepares the request to start
// the SyncGroup sync.
func (iSt *initiationState) prepareSGDeltaReq(ctx *context.T) error {
	iSt.config.sync.thLock.Lock()
	defer iSt.config.sync.thLock.Unlock()

	if err := iSt.config.sync.checkptLocalGen(ctx, iSt.config.appName, iSt.config.dbName, iSt.config.sgIds); err != nil {
		return err
	}

	var err error
	iSt.local, _, err = iSt.config.sync.copyDbGenInfo(ctx, iSt.config.appName, iSt.config.dbName, iSt.config.sgIds)
	if err != nil {
		return err
	}

	// Send request.
	req := interfaces.SgDeltaReq{
		AppName: iSt.config.appName,
		DbName:  iSt.config.dbName,
		InitVec: iSt.local,
	}

	iSt.req = interfaces.DeltaReqSgs{req}

	vlog.VI(4).Infof("sync: prepareSGDeltaReq: request: %v", req)

	return nil
}

// connectToPeer attempts to connect to the remote peer using the mount tables
// obtained from all the common SyncGroups.
func (iSt *initiationState) connectToPeer(ctx *context.T) bool {
	if len(iSt.config.mtTables) < 1 {
		vlog.Errorf("sync: connectToPeer: no mount tables found to connect to peer %s, app %s db %s", iSt.config.peer, iSt.config.appName, iSt.config.dbName)
		return false
	}

	for i, mt := range iSt.config.mtTables {
		absName := naming.Join(mt, iSt.config.peer, util.SyncbaseSuffix)
		c := interfaces.SyncClient(absName)
		var err error
		iSt.stream, err = c.GetDeltas(ctx, iSt.req, iSt.config.sync.name)
		if err == nil {
			vlog.VI(4).Infof("sync: connectToPeer: established on %s", absName)

			// Prune out the unsuccessful mount tables.
			iSt.config.mtTables = iSt.config.mtTables[i:]
			return true
		}
	}
	iSt.config.mtTables = nil
	vlog.Errorf("sync: connectToPeer: couldn't connect to peer %s", iSt.config.peer)
	return false
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

	// TODO(hpucha): See if we can avoid committing the entire delta stream
	// as one batch. Currently the dependency is between the log records and
	// the batch info.
	tx := iSt.config.st.NewTransaction()
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
		case interfaces.DeltaRespRespVec:
			iSt.remote = v.Value

		case interfaces.DeltaRespRec:
			// Insert log record in Database.
			// TODO(hpucha): Should we reserve more positions in a batch?
			// TODO(hpucha): Handle if SyncGroup is left/destroyed while sync is in progress.
			var pos uint64
			if iSt.sg {
				pos = iSt.config.sync.reservePosInDbLog(ctx, iSt.config.appName, iSt.config.dbName, v.Value.Metadata.ObjId, 1)
			} else {
				pos = iSt.config.sync.reservePosInDbLog(ctx, iSt.config.appName, iSt.config.dbName, "", 1)
			}

			rec := &localLogRec{Metadata: v.Value.Metadata, Pos: pos}
			batchId := rec.Metadata.BatchId
			if batchId != NoBatchId {
				if cnt, ok := batchMap[batchId]; !ok {
					if iSt.config.sync.startBatch(ctx, tx, batchId) != batchId {
						return verror.New(verror.ErrInternal, ctx, "failed to create batch info")
					}
					batchMap[batchId] = rec.Metadata.BatchCount
				} else if cnt != rec.Metadata.BatchCount {
					return verror.New(verror.ErrInternal, ctx, "inconsistent counts for tid", batchId, cnt, rec.Metadata.BatchCount)
				}
			}

			vlog.VI(4).Infof("sync: recvAndProcessDeltas: processing rec %v", rec)
			if err := iSt.insertRecInLogAndDag(ctx, rec, batchId, tx); err != nil {
				return err
			}

			if iSt.sg {
				// Add the SyncGroup value to the Database.
			} else {
				if err := iSt.insertRecInDb(ctx, rec, v.Value.Value, tx); err != nil {
					return err
				}
				// Check for BlobRefs, and process them.
				if err := iSt.processBlobRefs(ctx, &rec.Metadata, v.Value.Value); err != nil {
					return err
				}
			}

			// Mark object dirty.
			iSt.updObjects[rec.Metadata.ObjId] = &objConflictState{}
		}
	}

	if err := rcvr.Err(); err != nil {
		return err
	}

	// End the started batches if any.
	for bid, cnt := range batchMap {
		if err := iSt.config.sync.endBatch(ctx, tx, bid, cnt); err != nil {
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
		vlog.Fatalf("sync: recvAndProcessDeltas: encountered concurrent transaction")
	}
	if err == nil {
		committed = true
	}
	return err
}

// insertRecInLogAndDag adds a new log record to log and dag data structures.
func (iSt *initiationState) insertRecInLogAndDag(ctx *context.T, rec *localLogRec, batchId uint64, tx store.Transaction) error {
	var pfx string
	m := rec.Metadata

	if iSt.sg {
		pfx = m.ObjId
	} else {
		pfx = logDataPrefix
	}

	if err := putLogRec(ctx, tx, pfx, rec); err != nil {
		return err
	}
	logKey := logRecKey(pfx, m.Id, m.Gen)

	var err error
	switch m.RecType {
	case interfaces.NodeRec:
		err = iSt.config.sync.addNode(ctx, tx, m.ObjId, m.CurVers, logKey, m.Delete, m.Parents, m.BatchId, iSt.dagGraft)
	case interfaces.LinkRec:
		err = iSt.config.sync.addParent(ctx, tx, m.ObjId, m.CurVers, m.Parents[0], iSt.dagGraft)
	default:
		err = verror.New(verror.ErrInternal, ctx, "unknown log record type")
	}

	return err
}

// insertRecInDb inserts the versioned value in the Database.
func (iSt *initiationState) insertRecInDb(ctx *context.T, rec *localLogRec, valbuf []byte, tx store.Transaction) error {
	m := rec.Metadata
	// TODO(hpucha): Hack right now. Need to change Database's handling of
	// deleted objects. Currently, the initiator needs to treat deletions
	// specially since deletions do not get a version number or a special
	// value in the Database.
	if !rec.Metadata.Delete && rec.Metadata.RecType == interfaces.NodeRec {
		return watchable.PutAtVersion(ctx, tx, []byte(m.ObjId), valbuf, []byte(m.CurVers))
	}
	return nil
}

func (iSt *initiationState) processBlobRefs(ctx *context.T, m *interfaces.LogRecMetadata, valbuf []byte) error {
	objid := m.ObjId
	srcPeer := syncbaseIdToName(m.Id)

	vlog.VI(4).Infof("sync: processBlobRefs: begin processing blob refs for objid %s", objid)
	defer vlog.VI(4).Infof("sync: processBlobRefs: end processing blob refs for objid %s", objid)

	if valbuf == nil {
		return nil
	}

	var val *vdl.Value
	if err := vom.Decode(valbuf, &val); err != nil {
		return err
	}

	brs := make(map[nosql.BlobRef]struct{})
	if err := extractBlobRefs(val, brs); err != nil {
		return err
	}
	sgIds := make(sgSet)
	for br := range brs {
		for p, sgs := range iSt.config.sgPfxs {
			if strings.HasPrefix(extractAppKey(objid), p) {
				for sg := range sgs {
					sgIds[sg] = struct{}{}
				}
			}
		}
		vlog.VI(4).Infof("sync: processBlobRefs: Found blobref %v peer %v, source %v, sgs %v", br, iSt.config.peer, srcPeer, sgIds)
		info := &blobLocInfo{peer: iSt.config.peer, source: srcPeer, sgIds: sgIds}
		if err := iSt.config.sync.addBlobLocInfo(ctx, br, info); err != nil {
			return err
		}
	}

	return nil
}

// TODO(hpucha): Handle blobrefs part of list, map, any.
func extractBlobRefs(val *vdl.Value, brs map[nosql.BlobRef]struct{}) error {
	if val == nil {
		return nil
	}
	switch val.Kind() {
	case vdl.String:
		// Could be a BlobRef.
		var br nosql.BlobRef
		if val.Type() == vdl.TypeOf(br) {
			brs[nosql.BlobRef(val.RawString())] = struct{}{}
		}
	case vdl.Struct:
		for i := 0; i < val.Type().NumField(); i++ {
			v := val.StructField(i)
			if err := extractBlobRefs(v, brs); err != nil {
				return err
			}
		}
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
		vlog.VI(4).Infof("sync: processUpdatedObjects: begin: %d objects updated", len(iSt.updObjects))

		iSt.tx = iSt.config.st.NewTransaction()
		watchable.SetTransactionFromSync(iSt.tx) // for echo-suppression

		if count, err := iSt.detectConflicts(ctx); err != nil {
			return err
		} else {
			vlog.VI(4).Infof("sync: processUpdatedObjects: %d conflicts detected", count)
		}

		if err := iSt.resolveConflicts(ctx); err != nil {
			return err
		}

		err := iSt.updateDbAndSyncSt(ctx)
		if err == nil {
			err = iSt.tx.Commit()
		}
		if err == nil {
			committed = true
			// Update in-memory genvector since commit is successful.
			if err := iSt.config.sync.putDbGenInfoRemote(ctx, iSt.config.appName, iSt.config.dbName, iSt.sg, iSt.updLocal); err != nil {
				vlog.Fatalf("sync: processUpdatedObjects: putting geninfo in memory failed for app %s db %s, err %v", iSt.config.appName, iSt.config.dbName, err)
			}
			vlog.VI(4).Info("sync: processUpdatedObjects: end: changes committed")
			return nil
		}
		if verror.ErrorID(err) != store.ErrConcurrentTransaction.ID {
			return err
		}

		// Either updateDbAndSyncSt() or tx.Commit() detected a
		// concurrent transaction. Retry processing the remote updates.
		//
		// TODO(hpucha): Sleeping and retrying is a temporary
		// solution. Next iteration will have coordination with watch
		// thread to intelligently retry. Hence this value is not a
		// config param.
		vlog.VI(4).Info("sync: processUpdatedObjects: retry due to local mutations")
		iSt.tx.Abort()
		time.Sleep(1 * time.Second)
	}
}

// detectConflicts iterates through all the updated objects to detect conflicts.
func (iSt *initiationState) detectConflicts(ctx *context.T) (int, error) {
	count := 0
	for objid, confSt := range iSt.updObjects {
		// Check if object has a conflict.
		var err error
		confSt.isConflict, confSt.newHead, confSt.oldHead, confSt.ancestor, err = hasConflict(ctx, iSt.tx, objid, iSt.dagGraft)
		if err != nil {
			return 0, err
		}

		if !confSt.isConflict {
			if confSt.newHead == confSt.oldHead {
				confSt.res = &conflictResolution{ty: pickLocal}
			} else {
				confSt.res = &conflictResolution{ty: pickRemote}
			}
		} else {
			count++
		}
	}
	return count, nil
}

// updateDbAndSync updates the Database, and if that is successful, updates log,
// dag and genvector data structures as needed.
func (iSt *initiationState) updateDbAndSyncSt(ctx *context.T) error {
	for objid, confSt := range iSt.updObjects {
		// If the local version is picked, no further updates to the
		// Database are needed. If the remote version is picked or if a
		// new version is created, we put it in the Database.
		if confSt.res.ty != pickLocal && !iSt.sg {

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
					vlog.VI(4).Infof("sync: updateDbAndSyncSt: concurrent updates %s %s", version, confSt.oldHead)
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
					vlog.VI(4).Infof("sync: updateDbAndSyncSt: PutAtVersion %s %s", objid, newVersion)
					if err := watchable.PutAtVersion(ctx, iSt.tx, []byte(objid), confSt.res.val, []byte(newVersion)); err != nil {
						return err
					}
				}
				vlog.VI(4).Infof("sync: updateDbAndSyncSt: PutVersion %s %s", objid, newVersion)
				if err := watchable.PutVersion(ctx, iSt.tx, []byte(objid), []byte(newVersion)); err != nil {
					return err
				}
			} else {
				vlog.VI(4).Infof("sync: updateDbAndSyncSt: Deleting obj %s", objid)
				if err := iSt.tx.Delete([]byte(objid)); err != nil {
					return err
				}
			}
		}
		// Always update sync state irrespective of local/remote/new
		// versions being picked.
		if err := iSt.updateLogAndDag(ctx, objid); err != nil {
			return err
		}
	}

	return iSt.updateSyncSt(ctx)
}

// updateLogAndDag updates the log and dag data structures.
func (iSt *initiationState) updateLogAndDag(ctx *context.T, objid string) error {
	confSt, ok := iSt.updObjects[objid]
	if !ok {
		return verror.New(verror.ErrInternal, ctx, "object state not found", objid)
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
			rec = iSt.createLocalLinkLogRec(ctx, objid, confSt.oldHead, confSt.newHead)
			newVersion = confSt.oldHead
		case confSt.res.ty == pickRemote:
			// Remote version was picked as the conflict resolution.
			rec = iSt.createLocalLinkLogRec(ctx, objid, confSt.newHead, confSt.oldHead)
			newVersion = confSt.newHead
		default:
			// New version was created to resolve the conflict.
			rec = confSt.res.rec
			newVersion = confSt.res.rec.Metadata.CurVers
		}

		var pfx string
		if iSt.sg {
			pfx = objid
		} else {
			pfx = logDataPrefix
		}
		if err := putLogRec(ctx, iSt.tx, pfx, rec); err != nil {
			return err
		}

		// Add a new DAG node.
		var err error
		m := rec.Metadata
		switch m.RecType {
		case interfaces.NodeRec:
			err = iSt.config.sync.addNode(ctx, iSt.tx, objid, m.CurVers, logRecKey(pfx, m.Id, m.Gen), m.Delete, m.Parents, NoBatchId, nil)
		case interfaces.LinkRec:
			err = iSt.config.sync.addParent(ctx, iSt.tx, objid, m.CurVers, m.Parents[0], nil)
		default:
			return verror.New(verror.ErrInternal, ctx, "unknown log record type")
		}
		if err != nil {
			return err
		}
	}

	// Move the head. This should be idempotent. We may move head to the
	// local head in some cases.
	return moveHead(ctx, iSt.tx, objid, newVersion)
}

func (iSt *initiationState) createLocalLinkLogRec(ctx *context.T, objid, vers, par string) *localLogRec {
	vlog.VI(4).Infof("sync: createLocalLinkLogRec: obj %s vers %s par %s", objid, vers, par)

	var gen, pos uint64
	if iSt.sg {
		gen, pos = iSt.config.sync.reserveGenAndPosInDbLog(ctx, iSt.config.appName, iSt.config.dbName, objid, 1)
	} else {
		gen, pos = iSt.config.sync.reserveGenAndPosInDbLog(ctx, iSt.config.appName, iSt.config.dbName, "", 1)
	}

	rec := &localLogRec{
		Metadata: interfaces.LogRecMetadata{
			Id:      iSt.config.sync.id,
			Gen:     gen,
			RecType: interfaces.LinkRec,

			ObjId:      objid,
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
	dsInMem, err := iSt.config.sync.copyDbSyncStateInMem(ctx, iSt.config.appName, iSt.config.dbName)
	if err != nil {
		return err
	}
	// Create the state to be persisted.
	ds := &dbSyncState{
		Data: localGenInfo{
			Gen:        dsInMem.data.gen,
			CheckptGen: dsInMem.data.checkptGen,
		},
		Sgs:      make(map[interfaces.GroupId]localGenInfo),
		GenVec:   dsInMem.genvec,
		SgGenVec: dsInMem.sggenvec,
	}

	for id, info := range dsInMem.sgs {
		ds.Sgs[id] = localGenInfo{
			Gen:        info.gen,
			CheckptGen: info.checkptGen,
		}
	}

	genvec := ds.GenVec
	if iSt.sg {
		genvec = ds.SgGenVec
	}
	// remote can be a subset of local.
	for rpfx, respgv := range iSt.remote {
		for lpfx, lpgv := range genvec {
			if strings.HasPrefix(lpfx, rpfx) {
				mergePrefixGenVectors(lpgv, respgv)
			}
		}
		if _, ok := genvec[rpfx]; !ok {
			genvec[rpfx] = respgv
		}
	}

	iSt.updLocal = genvec
	// Clean the genvector of any local state. Note that local state is held
	// in gen/ckPtGen in sync state struct.
	for _, pgv := range iSt.updLocal {
		delete(pgv, iSt.config.sync.id)
	}

	// TODO(hpucha): Flip join pending if needed.

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
