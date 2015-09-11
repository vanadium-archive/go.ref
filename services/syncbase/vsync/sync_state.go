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
//
// Sync also tracks the current generation number and the current local log
// position for each mutation of a SyncGroup, created on a Database. Similar to
// the data log records, these log records are used to sync SyncGroup metadata.
//
// The generations for the data mutations and mutations for each SyncGroup are
// in separate spaces. Data mutations in a Database start at gen 1, and
// grow. Mutations for each SyncGroup start at gen 1, and grow. Thus, for the
// local data log records, the keys are of the form
// $sync:log:data:<devid>:<gen>, and the keys for local SyncGroup log record are
// of the form $sync:log:<sgid>:<devid>:<gen>.

// TODO(hpucha): Should this space be separate from the data or not? If it is
// not, it can provide consistency between data and SyncGroup metadata. For
// example, lets say we mutate the data in a SyncGroup and soon after change the
// SyncGroup ACL to prevent syncing with a device. This device may not get the
// last batch of updates since the next time it will try to sync, it will be
// rejected. However implementing consistency is not straightforward. Even if we
// had SyncGroup updates in the same space as the data, we need to switch to the
// right SyncGroup ACL at the responder based on the requested generations.

import (
	"fmt"
	"strconv"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// localGenInfoInMem represents the state corresponding to local generations.
type localGenInfoInMem struct {
	gen        uint64
	pos        uint64
	checkptGen uint64
}

func (in *localGenInfoInMem) deepCopy() *localGenInfoInMem {
	out := &localGenInfoInMem{
		gen:        in.gen,
		pos:        in.pos,
		checkptGen: in.checkptGen,
	}
	return out
}

// dbSyncStateInMem represents the in-memory sync state of a Database and all
// its SyncGroups.
type dbSyncStateInMem struct {
	data *localGenInfoInMem                        // info for data.
	sgs  map[interfaces.GroupId]*localGenInfoInMem // info for SyncGroups.

	// Note: Generation vector contains state from remote devices only.
	genvec   interfaces.GenVector
	sggenvec interfaces.GenVector
}

func (in *dbSyncStateInMem) deepCopy() *dbSyncStateInMem {
	out := &dbSyncStateInMem{}
	out.data = in.data.deepCopy()

	out.sgs = make(map[interfaces.GroupId]*localGenInfoInMem)
	for id, info := range in.sgs {
		out.sgs[id] = info.deepCopy()
	}

	out.genvec = in.genvec.DeepCopy()
	out.sggenvec = in.sggenvec.DeepCopy()

	return out
}

// initSync initializes the sync module during startup. It scans all the
// databases across all apps to initialize the following:
// a) in-memory sync state of a Database and all its SyncGroups consisting of
// the current generation number, log position and generation vector.
// b) watcher map of prefixes currently being synced.
// c) republish names in mount tables for all syncgroups.
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
				scanStart, scanLimit = util.ScanPrefixArgs(logRecKey(logDataPrefix, s.id, ds.Data.Gen), "")
			}
			var maxpos uint64
			var dbName string
			// Scan local log records to find the most recent one.
			st.Scan(scanStart, scanLimit)
			// Scan remote log records using the persisted GenVector.
			s.syncState[dbName] = &dbSyncStateInMem{data: &localGenInfoInMem{pos: maxpos + 1}}
		}

		return false
	})

	return errFinal
}

// Note: For all the utilities below, if the sgid parameter is non-nil, the
// operation is performed in the SyncGroup space. If nil, it is performed in the
// data space for the Database.
//
// TODO(hpucha): Once GroupId is changed to string, clean up these function
// signatures.

// reserveGenAndPosInDbLog reserves a chunk of generation numbers and log
// positions in a Database's log. Used when local updates result in log
// entries.
func (s *syncService) reserveGenAndPosInDbLog(ctx *context.T, appName, dbName, sgid string, count uint64) (uint64, uint64) {
	return s.reserveGenAndPosInternal(appName, dbName, sgid, count, count)
}

// reservePosInDbLog reserves a chunk of log positions in a Database's log. Used
// when remote log records are received.
func (s *syncService) reservePosInDbLog(ctx *context.T, appName, dbName, sgid string, count uint64) uint64 {
	_, pos := s.reserveGenAndPosInternal(appName, dbName, sgid, 0, count)
	return pos
}

func (s *syncService) reserveGenAndPosInternal(appName, dbName, sgid string, genCount, posCount uint64) (uint64, uint64) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		ds = &dbSyncStateInMem{
			data: &localGenInfoInMem{gen: 1},
			sgs:  make(map[interfaces.GroupId]*localGenInfoInMem),
		}
		s.syncState[name] = ds
	}

	var info *localGenInfoInMem
	if sgid != "" {
		id, err := strconv.ParseUint(sgid, 10, 64)
		if err != nil {
			vlog.Fatalf("sync: reserveGenAndPosInternal: invalid syncgroup id", sgid)
		}

		var ok bool
		info, ok = ds.sgs[interfaces.GroupId(id)]
		if !ok {
			info = &localGenInfoInMem{gen: 1}
			ds.sgs[interfaces.GroupId(id)] = info
		}
	} else {
		info = ds.data
	}
	gen := info.gen
	pos := info.pos

	info.gen += genCount
	info.pos += posCount

	return gen, pos
}

// checkptLocalGen freezes the local generation number for the responder's use.
func (s *syncService) checkptLocalGen(ctx *context.T, appName, dbName string, sgs sgSet) error {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	// The frozen generation is the last generation number used, i.e. one
	// below the next available one to use.
	if len(sgs) > 0 {
		// Checkpoint requested SyncGroups.
		for id := range sgs {
			info, ok := ds.sgs[id]
			if !ok {
				return verror.New(verror.ErrInternal, ctx, "sg state not found", name, id)
			}
			info.checkptGen = info.gen - 1
		}
	} else {
		ds.data.checkptGen = ds.data.gen - 1
	}
	return nil
}

// initSyncStateInMem initializes the in memory sync state of the Database/SyncGroup if needed.
func (s *syncService) initSyncStateInMem(ctx *context.T, appName, dbName string, sgid interfaces.GroupId) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	if s.syncState[name] == nil {
		s.syncState[name] = &dbSyncStateInMem{
			data: &localGenInfoInMem{gen: 1},
			sgs:  make(map[interfaces.GroupId]*localGenInfoInMem),
		}
	}
	if sgid != interfaces.NoGroupId {
		ds := s.syncState[name]
		if _, ok := ds.sgs[sgid]; !ok {
			ds.sgs[sgid] = &localGenInfoInMem{gen: 1}
		}
	}
	return
}

// copyDbSyncStateInMem returns a copy of the current in memory sync state of the Database.
func (s *syncService) copyDbSyncStateInMem(ctx *context.T, appName, dbName string) (*dbSyncStateInMem, error) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return nil, verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}
	return ds.deepCopy(), nil
}

// copyDbGenInfo returns a copy of the current generation information of the Database.
func (s *syncService) copyDbGenInfo(ctx *context.T, appName, dbName string, sgs sgSet) (interfaces.GenVector, uint64, error) {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return nil, 0, verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	var genvec interfaces.GenVector
	var gen uint64
	if len(sgs) > 0 {
		genvec = make(interfaces.GenVector)
		for id := range sgs {
			sid := fmt.Sprintf("%d", id)
			gv := ds.sggenvec[sid]
			genvec[sid] = gv.DeepCopy()
			genvec[sid][s.id] = ds.sgs[id].checkptGen
		}
	} else {
		genvec = ds.genvec.DeepCopy()

		// Add local generation information to the genvec.
		for _, gv := range genvec {
			gv[s.id] = ds.data.checkptGen
		}
		gen = ds.data.checkptGen
	}
	return genvec, gen, nil
}

// putDbGenInfoRemote puts the current remote generation information of the Database.
func (s *syncService) putDbGenInfoRemote(ctx *context.T, appName, dbName string, sg bool, genvec interfaces.GenVector) error {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	name := appDbName(appName, dbName)
	ds, ok := s.syncState[name]
	if !ok {
		return verror.New(verror.ErrInternal, ctx, "db state not found", name)
	}

	if sg {
		ds.sggenvec = genvec.DeepCopy()
	} else {
		ds.genvec = genvec.DeepCopy()
	}

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

////////////////////////////////////////////////////////////
// Low-level utility functions to access sync state.

// dbSyncStateKey returns the key used to access the sync state of a Database.
func dbSyncStateKey() string {
	return util.JoinKeyParts(util.SyncPrefix, dbssPrefix)
}

// putDbSyncState persists the sync state object for a given Database.
func putDbSyncState(ctx *context.T, tx store.Transaction, ds *dbSyncState) error {
	return util.Put(ctx, tx, dbSyncStateKey(), ds)
}

// getDbSyncState retrieves the sync state object for a given Database.
func getDbSyncState(ctx *context.T, st store.StoreReader) (*dbSyncState, error) {
	var ds dbSyncState
	if err := util.Get(ctx, st, dbSyncStateKey(), &ds); err != nil {
		return nil, err
	}
	return &ds, nil
}

////////////////////////////////////////////////////////////
// Low-level utility functions to access log records.

// logRecsPerDeviceScanPrefix returns the prefix used to scan log records for a particular device.
func logRecsPerDeviceScanPrefix(id uint64) string {
	return util.JoinKeyParts(util.SyncPrefix, logPrefix, fmt.Sprintf("%x", id))
}

// logRecKey returns the key used to access a specific log record.
func logRecKey(pfx string, id, gen uint64) string {
	return util.JoinKeyParts(util.SyncPrefix, logPrefix, pfx, fmt.Sprintf("%d", id), fmt.Sprintf("%016x", gen))
}

// splitLogRecKey is the inverse of logRecKey and returns the prefix, device id
// and generation number.
func splitLogRecKey(ctx *context.T, key string) (string, uint64, uint64, error) {
	parts := util.SplitKeyParts(key)
	verr := verror.New(verror.ErrInternal, ctx, "invalid logreckey", key)
	if len(parts) != 5 {
		return "", 0, 0, verr
	}
	if parts[0] != util.SyncPrefix || parts[1] != logPrefix {
		return "", 0, 0, verr
	}
	if parts[2] != logDataPrefix {
		if _, err := strconv.ParseUint(parts[2], 10, 64); err != nil {
			return "", 0, 0, verr
		}
	}
	id, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil {
		return "", 0, 0, verr
	}
	gen, err := strconv.ParseUint(parts[4], 16, 64)
	if err != nil {
		return "", 0, 0, verr
	}
	return parts[2], id, gen, nil
}

// hasLogRec returns true if the log record for (devid, gen) exists.
func hasLogRec(st store.StoreReader, pfx string, id, gen uint64) (bool, error) {
	// TODO(hpucha): optimize to avoid the unneeded fetch/decode of the data.
	var rec localLogRec
	if err := util.Get(nil, st, logRecKey(pfx, id, gen), &rec); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			err = nil
		}
		return false, err
	}
	return true, nil
}

// putLogRec stores the log record.
func putLogRec(ctx *context.T, tx store.Transaction, pfx string, rec *localLogRec) error {
	return util.Put(ctx, tx, logRecKey(pfx, rec.Metadata.Id, rec.Metadata.Gen), rec)
}

// getLogRec retrieves the log record for a given (devid, gen).
func getLogRec(ctx *context.T, st store.StoreReader, pfx string, id, gen uint64) (*localLogRec, error) {
	return getLogRecByKey(ctx, st, logRecKey(pfx, id, gen))
}

// getLogRecByKey retrieves the log record for a given log record key.
func getLogRecByKey(ctx *context.T, st store.StoreReader, key string) (*localLogRec, error) {
	var rec localLogRec
	if err := util.Get(ctx, st, key, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// delLogRec deletes the log record for a given (devid, gen).
func delLogRec(ctx *context.T, tx store.Transaction, pfx string, id, gen uint64) error {
	return util.Delete(ctx, tx, logRecKey(pfx, id, gen))
}
