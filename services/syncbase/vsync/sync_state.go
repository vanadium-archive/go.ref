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
	"fmt"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"

	"v.io/v23/context"
	"v.io/v23/verror"
)

// dbSyncStateInMem represents the in-memory sync state of a Database.
type dbSyncStateInMem struct {
	gen    uint64
	pos    uint64
	genvec interfaces.GenVector
}

// initSync initializes the sync module during startup. It scans all the
// databases across all apps to a) initialize the in-memory sync state of a
// Database consisting of the current generation number, log position and
// generation vector; b) initialize the prefixes that are currently being
// synced.
//
// TODO(hpucha): This is incomplete. Flesh this out further.
func (s *syncService) initSync(ctx *context.T) error {
	s.syncStateLock.Lock()
	defer s.syncStateLock.Unlock()

	var errFinal error
	s.syncState = make(map[string]*dbSyncStateInMem)

	// TODO(hpucha): Temporary hack.
	return errFinal

	/*s.forEachDatabaseStore(ctx, func(st store.Store) bool {
		sn := st.NewSnapshot()
		defer sn.Close()

		ds, err := getDbSyncState(ctx, sn)
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
		sn.Scan(scanStart, scanLimit)

		// Scan remote log records using the persisted GenVector.

		s.syncState[dbName] = &dbSyncStateInMem{pos: maxpos + 1}

		return false
	})

	return errFinal*/
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

// appDbName combines the app and db names to return a globally unique name for
// a Database.  This relies on the fact that the app name is globally unique and
// the db name is unique within the scope of the app.
func appDbName(appName, dbName string) string {
	return util.JoinKeyParts(appName, dbName)
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
		return nil, verror.New(verror.ErrInternal, ctx, err)
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
		return nil, verror.New(verror.ErrInternal, ctx, err)
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
