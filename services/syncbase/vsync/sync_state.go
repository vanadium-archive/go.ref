// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// New log records are created when objects in the local store are created,
// updated or deleted. Local log records are also replayed to keep the
// per-object dags consistent with the local store state. Sync module assigns
// each log record created within a Database a unique sequence number, called
// the generation number. Locally on each device, the position of each log
// record is also recorded relative to other local and remote log records.
//
// When a device receives a request to send log records, it first computes the
// missing generations between itself and the incoming request on a per-prefix
// basis. It then sends all the log records belonging to the missing generations
// in the order they occur locally (using the local log position). A device that
// receives log records over the network replays all the records received from
// another device in a single batch. Each replayed log record adds a new version
// to the dag of the object contained in the log record. At the end of replaying
// all the log records, conflict detection and resolution is carried out for all
// the objects learned during this iteration. Conflict detection and resolution
// is carried out after a batch of log records are replayed, instead of
// incrementally after each record is replayed, to avoid repeating conflict
// resolution already performed by other devices.
//
// Sync module tracks the current generation number and the current local log
// position for each Database. In addition, it also tracks the current
// generation vector for a Database. Log records are indexed such that they can
// be selectively retrieved from the store for any missing generation from any
// device.

import (
	"container/heap"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

// dbSyncStateInMem represents the in-memory sync state of a Database.
type dbSyncStateInMem struct {
	gen uint64
	pos uint64

	ckPtGen uint64
	genvec  interfaces.GenVector // Note: Generation vector contains state from remote devices only.
}

// initSync initializes the sync module during startup. It scans all the
// databases across all apps to initialize the following:
// a) in-memory sync state of a Database consisting of the current generation
//    number, log position and generation vector.
// b) watcher map of prefixes currently being synced.
//
// TODO(hpucha): This is incomplete. Flesh this out further.
func (s *syncService) initSync(ctx *context.T) error {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	var errFinal error
	s.syncState = make(map[string]*dbSyncStateInMem)

	s.forEachDatabaseStore(ctx, func(appName, dbName string, st store.Store) bool {
		// Scan the SyncGroups, skipping those not yet being watched.
		forEachSyncGroup(st, func(sg *interfaces.SyncGroup) bool {
			// TODO(rdaoud): only use SyncGroups that have been
			// marked as "watchable" by the sync watcher thread.
			// This is to handle the case of a SyncGroup being
			// created but Syncbase restarting before the watcher
			// processed the SyncGroupOp entry in the watch queue.
			// It should not be syncing that SyncGroup's data after
			// restart, but wait until the watcher processes the
			// entry as would have happened without a restart.
			for _, prefix := range sg.Spec.Prefixes {
				incrWatchPrefix(appName, dbName, prefix)
			}
			return false
		})

		if false {
			// Fetch the sync state.
			ds, err := getDbSyncState(ctx, st)
			if err != nil && verror.ErrorID(err) != verror.ErrNoExist.ID {
				errFinal = err
				return false
			}
			var scanStart, scanLimit []byte
			// Figure out what to scan among local log records.
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				scanStart, scanLimit = util.ScanPrefixArgs(logRecsPerDeviceScanPrefix(s.id), "")
			} else {
				scanStart, scanLimit = util.ScanPrefixArgs(logRecKey(s.id, ds.Gen), "")
			}
			var maxpos uint64
			var dbName string
			// Scan local log records to find the most recent one.
			st.Scan(scanStart, scanLimit)
			// Scan remote log records using the persisted GenVector.
			s.syncState[dbName] = &dbSyncStateInMem{pos: maxpos + 1}
		}

		return false
	})

	return errFinal
}

// reserveGenAndPosInDbLog reserves a chunk of generation numbers and log
// positions in a Database's log. Used when local updates result in log
// entries.
func (s *syncService) reserveGenAndPosInDbLog(ctx *context.T, appName, dbName string, count uint64) (uint64, uint64) {
	return s.reserveGenAndPosInternal(appName, dbName, count, count)
}

// reservePosInDbLog reserves a chunk of log positions in a Database's log. Used
// when remote log records are received.
func (s *syncService) reservePosInDbLog(ctx *context.T, appName, dbName string, count uint64) uint64 {
	_, pos := s.reserveGenAndPosInternal(appName, dbName, 0, count)
	return pos
}

func (s *syncService) reserveGenAndPosInternal(appName, dbName string, genCount, posCount uint64) (uint64, uint64) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		ds = &dbSyncStateInMem{gen: 1}
		s.syncState[name] = ds
	}

	gen := ds.gen
	pos := ds.pos

	ds.gen += genCount
	ds.pos += posCount

	return gen, pos
}

// checkPtLocalGen freezes the local generation number for the responder's use.
func (s *syncService) checkPtLocalGen(ctx *context.T, appName, dbName string) error {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	ds.ckPtGen = ds.gen
	return nil
}

// getDbSyncStateInMem returns a copy of the current in memory sync state of the Database.
func (s *syncService) getDbSyncStateInMem(ctx *context.T, appName, dbName string) (*dbSyncStateInMem, error) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return nil, verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	dsCopy := &dbSyncStateInMem{
		gen:     ds.gen,
		pos:     ds.pos,
		ckPtGen: ds.ckPtGen,
	}

	// Make a copy of the genvec.
	dsCopy.genvec = copyGenVec(ds.genvec)

	return dsCopy, nil
}

// getDbGenInfo returns a copy of the current generation information of the Database.
func (s *syncService) getDbGenInfo(ctx *context.T, appName, dbName string) (interfaces.GenVector, uint64, error) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return nil, 0, verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	// Make a copy of the genvec.
	genvec := copyGenVec(ds.genvec)

	// Add local generation information to the genvec.
	for _, gv := range genvec {
		gv[s.id] = ds.ckPtGen
	}

	return genvec, ds.ckPtGen, nil
}

// putDbGenInfoRemote puts the current remote generation information of the Database.
func (s *syncService) putDbGenInfoRemote(ctx *context.T, appName, dbName string, genvec interfaces.GenVector) error {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	// Make a copy of the genvec.
	ds.genvec = copyGenVec(genvec)

	return nil
}

// appDbName combines the app and db names to return a globally unique name for
// a Database.  This relies on the fact that the app name is globally unique and
// the db name is unique within the scope of the app.
func appDbName(appName, dbName string) string {
	return util.JoinKeyParts(appName, dbName)
}

// splitAppDbName is the inverse of appDbName and returns app and db name from a
// globally unique name for a Database.
func splitAppDbName(ctx *context.T, name string) (string, string, error) {
	parts := util.SplitKeyParts(name)
	if len(parts) != 2 {
		return "", "", verror.New(verror.ErrInternal, ctx, "invalid appDbName", name)
	}
	return parts[0], parts[1], nil
}

func copyGenVec(in interfaces.GenVector) interfaces.GenVector {
	genvec := make(interfaces.GenVector)
	for p, inpgv := range in {
		pgv := make(interfaces.PrefixGenVector)
		for id, gen := range inpgv {
			pgv[id] = gen
		}
		genvec[p] = pgv
	}
	return genvec
}

////////////////////////////////////////////////////////////
// Low-level utility functions to access sync state.

// dbSyncStateKey returns the key used to access the sync state of a Database.
func dbSyncStateKey() string {
	return util.JoinKeyParts(util.SyncPrefix, "dbss")
}

// putDbSyncState persists the sync state object for a given Database.
func putDbSyncState(ctx *context.T, tx store.StoreReadWriter, ds *dbSyncState) error {
	_ = tx.(store.Transaction)
	if err := util.PutObject(tx, dbSyncStateKey(), ds); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// getDbSyncState retrieves the sync state object for a given Database.
func getDbSyncState(ctx *context.T, st store.StoreReader) (*dbSyncState, error) {
	var ds dbSyncState
	if err := util.GetObject(st, dbSyncStateKey(), &ds); err != nil {
		return nil, translateError(ctx, err, dbSyncStateKey())
	}
	return &ds, nil
}

////////////////////////////////////////////////////////////
// Low-level utility functions to access log records.

// logRecsPerDeviceScanPrefix returns the prefix used to scan log records for a particular device.
func logRecsPerDeviceScanPrefix(id uint64) string {
	return util.JoinKeyParts(util.SyncPrefix, "log", fmt.Sprintf("%x", id))
}

// logRecKey returns the key used to access a specific log record.
func logRecKey(id, gen uint64) string {
	return util.JoinKeyParts(util.SyncPrefix, "log", fmt.Sprintf("%x", id), fmt.Sprintf("%016x", gen))
}

// splitLogRecKey is the inverse of logRecKey and returns device id and generation number.
func splitLogRecKey(ctx *context.T, key string) (uint64, uint64, error) {
	parts := util.SplitKeyParts(key)
	verr := verror.New(verror.ErrInternal, ctx, "invalid logreckey", key)
	if len(parts) != 4 {
		return 0, 0, verr
	}
	id, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return 0, 0, verr
	}
	gen, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil {
		return 0, 0, verr
	}
	return id, gen, nil
}

// hasLogRec returns true if the log record for (devid, gen) exists.
func hasLogRec(st store.StoreReader, id, gen uint64) bool {
	// TODO(hpucha): optimize to avoid the unneeded fetch/decode of the data.
	var rec localLogRec
	if err := util.GetObject(st, logRecKey(id, gen), &rec); err != nil {
		return false
	}
	return true
}

// putLogRec stores the log record.
func putLogRec(ctx *context.T, tx store.StoreReadWriter, rec *localLogRec) error {
	_ = tx.(store.Transaction)
	if err := util.PutObject(tx, logRecKey(rec.Metadata.Id, rec.Metadata.Gen), rec); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// getLogRec retrieves the log record for a given (devid, gen).
func getLogRec(ctx *context.T, st store.StoreReader, id, gen uint64) (*localLogRec, error) {
	var rec localLogRec
	if err := util.GetObject(st, logRecKey(id, gen), &rec); err != nil {
		return nil, translateError(ctx, err, logRecKey(id, gen))
	}
	return &rec, nil
}

// delLogRec deletes the log record for a given (devid, gen).
func delLogRec(ctx *context.T, tx store.StoreReadWriter, id, gen uint64) error {
	_ = tx.(store.Transaction)

	if err := tx.Delete([]byte(logRecKey(id, gen))); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

////////////////////////////////////////////////////////////
// Genvector-related utilities.

// sendDeltasPerDatabase sends to an initiator all the missing generations
// corresponding to the prefixes requested for this Database, and a genvector
// summarizing the knowledge transferred from the responder to the
// initiator. This happens in two phases:
//
// In the first phase, for a given set of nested prefixes from the initiator,
// the shortest prefix in that set is extracted. The initiator's prefix
// genvector for this shortest prefix represents the lower bound on its
// knowledge for the entire set of nested prefixes. This prefix genvector
// (representing the lower bound) is diffed with all the responder prefix
// genvectors corresponding to same or deeper prefixes compared to the initiator
// prefix. This diff produces a bound on the missing knowledge. For example, say
// the initiator is interested in prefixes {foo, foobar}, where each prefix is
// associated with a prefix genvector. Since the initiator strictly has as much
// or more knowledge for prefix "foobar" as it has for prefix "foo", "foo"'s
// prefix genvector is chosen as the lower bound for the initiator's
// knowledge. Similarly, say the responder has knowledge on prefixes {f,
// foobarX, foobarY, bar}. The responder diffs the prefix genvectors for
// prefixes f, foobarX and foobarY with the initiator's prefix genvector to
// compute a bound on missing generations (all responder's prefixes that match
// "foo". Note that since the responder doesn't have a prefix genvector at
// "foo", its knowledge at "f" is applicable to "foo").
//
// Since the first phase outputs an aggressive calculation of missing
// generations containing more generation entries than strictly needed by the
// initiator, in the second phase, each missing generation is sent to the
// initiator only if the initiator is eligible for it and is not aware of
// it. The generations are sent to the initiator in the same order as the
// responder learned them so that the initiator can reconstruct the DAG for the
// objects by learning older nodes first.
func (s *syncService) sendDeltasPerDatabase(ctx *context.T, call rpc.ServerCall, appName, dbName string, initVec interfaces.GenVector, stream logRecStream) (interfaces.GenVector, error) {
	// Phase 1 of sendDeltas. diff contains the bound on the generations
	// missing from the initiator per device.
	diff, outVec, err := s.computeDeltaBound(ctx, appName, dbName, initVec)
	if err != nil {
		return nil, err
	}

	// Phase 2 of sendDeltas: Process the diff, filtering out records that
	// are not needed, and send the remainder on the wire ordered.
	st, err := s.getDbStore(ctx, call, appName, dbName)
	if err != nil {
		return nil, err
	}

	// We now visit every log record in the generation range as obtained
	// from phase 1 in their log order. We use a heap to incrementally sort
	// the log records as per their position in the log.
	//
	// Init the min heap, one entry per device in the diff.
	mh := make(minHeap, 0, len(diff))
	for dev, r := range diff {
		r.cur = r.min
		rec, err := getNextLogRec(ctx, st, dev, r)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			mh = append(mh, rec)
		} else {
			delete(diff, dev)
		}
	}
	heap.Init(&mh)

	// Process the log records in order.
	initPfxs := extractAndSortPrefixes(initVec)

	for mh.Len() > 0 {
		rec := heap.Pop(&mh).(*localLogRec)

		if !filterLogRec(rec, initVec, initPfxs) {
			// Send on the wire.
			wireRec := interfaces.LogRec{Metadata: rec.Metadata}
			// TODO(hpucha): Hash out this fake stream stuff when
			// defining the RPC and the rest of the responder.
			stream.Send(wireRec)
		}

		// Add a new record from the same device if not done.
		dev := rec.Metadata.Id
		rec, err := getNextLogRec(ctx, st, dev, diff[dev])
		if err != nil {
			return nil, err
		}
		if rec != nil {
			heap.Push(&mh, rec)
		} else {
			delete(diff, dev)
		}
	}

	return outVec, nil
}

// computeDeltaBound computes the bound on missing generations across all
// requested prefixes (phase 1 of sendDeltas).
func (s *syncService) computeDeltaBound(ctx *context.T, appName, dbName string, initVec interfaces.GenVector) (genRangeVector, interfaces.GenVector, error) {
	respVec, respGen, err := s.getDbGenInfo(ctx, appName, dbName)
	if err != nil {
		return nil, nil, err
	}
	respPfxs := extractAndSortPrefixes(respVec)
	initPfxs := extractAndSortPrefixes(initVec)
	if len(initPfxs) == 0 {
		return nil, nil, verror.New(verror.ErrInternal, ctx, "empty initiator generation vector")
	}

	outVec := make(interfaces.GenVector)
	diff := make(genRangeVector)
	pfx := initPfxs[0]

	for _, p := range initPfxs {
		if strings.HasPrefix(p, pfx) && p != pfx {
			continue
		}

		// Process this prefix as this is the start of a new set of
		// nested prefixes.
		pfx = p

		// Lower bound on initiator's knowledge for this prefix set.
		initpgv := initVec[pfx]

		// Find the relevant responder prefixes and add the corresponding knowledge.
		var respgv interfaces.PrefixGenVector
		var rpStart string
		for _, rp := range respPfxs {
			if !strings.HasPrefix(rp, pfx) && !strings.HasPrefix(pfx, rp) {
				// No relationship with pfx.
				continue
			}

			if strings.HasPrefix(pfx, rp) {
				// If rp is a prefix of pfx, remember it because
				// it may be a potential starting point for the
				// responder's knowledge. The actual starting
				// point is the deepest prefix where rp is a
				// prefix of pfx.
				//
				// Say the initiator is looking for "foo", and
				// the responder has knowledge for "f" and "fo",
				// the responder's starting point will be the
				// prefix genvector for "fo". Similarly, if the
				// responder has knowledge for "foo", the
				// starting point will be the prefix genvector
				// for "foo".
				rpStart = rp
			} else {
				// If pfx is a prefix of rp, this knowledge must
				// be definitely sent to the initiator. Diff the
				// prefix genvectors to adjust the delta bound and
				// include in outVec.
				respgv = respVec[rp]
				s.diffPrefixGenVectors(respgv, initpgv, diff)
				outVec[rp] = respgv
			}
		}

		// Deal with the starting point.
		if rpStart == "" {
			// No matching prefixes for pfx were found.
			respgv = make(interfaces.PrefixGenVector)
			respgv[s.id] = respGen
		} else {
			respgv = respVec[rpStart]
		}
		s.diffPrefixGenVectors(respgv, initpgv, diff)
		outVec[pfx] = respgv
	}

	return diff, outVec, nil
}

// genRange represents a range of generations (min and max inclusive).
type genRange struct {
	min uint64
	max uint64
	cur uint64
}

type genRangeVector map[uint64]*genRange

// diffPrefixGenVectors diffs two generation vectors, belonging to the responder
// and the initiator, and updates the range of generations per device known to
// the responder but not known to the initiator. "gens" (generation range) is
// passed in as an input argument so that it can be incrementally updated as the
// range of missing generations grows when different responder prefix genvectors
// are used to compute the diff.
//
// For example: Generation vector for responder is say RVec = {A:10, B:5, C:1},
// Generation vector for initiator is say IVec = {A:5, B:10, D:2}. Diffing these
// two vectors returns: {A:[6-10], C:[1-1]}.
//
// TODO(hpucha): Add reclaimVec for GCing.
func (s *syncService) diffPrefixGenVectors(respPVec, initPVec interfaces.PrefixGenVector, gens genRangeVector) {
	// Compute missing generations for devices that are in both initiator's and responder's vectors.
	for devid, gen := range initPVec {
		rgen, ok := respPVec[devid]
		// Skip since responder doesn't know of this device.
		if ok {
			updateDevRange(devid, rgen, gen, gens)
		}
	}

	// Compute missing generations for devices not in initiator's vector but in responder's vector.
	for devid, rgen := range respPVec {
		if _, ok := initPVec[devid]; !ok {
			updateDevRange(devid, rgen, 0, gens)
		}
	}
}

func updateDevRange(devid, rgen, gen uint64, gens genRangeVector) {
	if gen < rgen {
		// Need to include all generations in the interval [gen+1,rgen], gen+1 and rgen inclusive.
		if r, ok := gens[devid]; !ok {
			gens[devid] = &genRange{min: gen + 1, max: rgen}
		} else {
			if gen+1 < r.min {
				r.min = gen + 1
			}
			if rgen > r.max {
				r.max = rgen
			}
		}
	}
}

func extractAndSortPrefixes(vec interfaces.GenVector) []string {
	pfxs := make([]string, len(vec))
	i := 0
	for p := range vec {
		pfxs[i] = p
		i++
	}
	sort.Strings(pfxs)
	return pfxs
}

// TODO(hpucha): This can be optimized using a scan instead of "gets" in a for
// loop.
func getNextLogRec(ctx *context.T, sn store.StoreReader, dev uint64, r *genRange) (*localLogRec, error) {
	for i := r.cur; i <= r.max; i++ {
		rec, err := getLogRec(ctx, sn, dev, i)
		if err == nil {
			r.cur = i + 1
			return rec, nil
		}
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return nil, err
		}
	}
	return nil, nil
}

// Note: initPfxs is sorted.
func filterLogRec(rec *localLogRec, initVec interfaces.GenVector, initPfxs []string) bool {
	filter := true

	var maxGen uint64
	for _, p := range initPfxs {
		if strings.HasPrefix(rec.Metadata.ObjId, p) {
			// Do not filter. Initiator is interested in this
			// prefix.
			filter = false

			// Track if the initiator knows of this record.
			gen := initVec[p][rec.Metadata.Id]
			if maxGen < gen {
				maxGen = gen
			}
		}
	}

	// Filter this record if the initiator already has it.
	if maxGen >= rec.Metadata.Gen {
		return true
	}

	return filter
}

// A minHeap implements heap.Interface and holds local log records.
type minHeap []*localLogRec

func (mh minHeap) Len() int { return len(mh) }

func (mh minHeap) Less(i, j int) bool {
	return mh[i].Pos < mh[j].Pos
}

func (mh minHeap) Swap(i, j int) {
	mh[i], mh[j] = mh[j], mh[i]
}

func (mh *minHeap) Push(x interface{}) {
	item := x.(*localLogRec)
	*mh = append(*mh, item)
}

func (mh *minHeap) Pop() interface{} {
	old := *mh
	n := len(old)
	item := old[n-1]
	*mh = old[0 : n-1]
	return item
}

type logRecStream interface {
	Send(interfaces.LogRec)
}
