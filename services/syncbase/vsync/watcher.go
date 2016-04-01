// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Syncbase Watcher is a goroutine that listens to local Database updates from
// applications and modifies sync metadata (e.g. DAG and local log records).
// The coupling between Syncbase storage and sync is loose, via asynchronous
// listening by the Watcher, to unblock the application operations as soon as
// possible, and offload the sync metadata update to the Watcher.  When the
// application mutates objects in a Database, additional entries are written
// to a log queue, persisted in the same Database.  This queue is read by the
// sync Watcher to learn of the changes.

import (
	"fmt"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/services/watch"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/watchable"
	sbwatchable "v.io/x/ref/services/syncbase/watchable"
)

var (
	// watchPrefixes is an in-memory cache of syncgroup prefixes for each
	// app database.  It is filled at startup from persisted syncgroup data
	// and updated at runtime when syncgroups are joined or left.  It is
	// not guarded by a mutex because only the watcher goroutine uses it
	// beyond the startup phase (before any sync goroutines are started).
	// The map keys are the appdb names (globally unique).
	watchPrefixes = make(map[string]sgPrefixes)
)

// sgPrefixes tracks syncgroup prefixes being synced in a database and their
// syncgroups.
type sgPrefixes map[string]sgSet

// watchStore processes updates obtained by watching the store.  This is the
// sync watcher goroutine that learns about store updates asynchronously by
// reading log records that track object mutation histories in each database.
// For each batch mutation, the watcher updates the sync DAG and log records.
// When an application makes a single non-transactional put, it is represented
// as a batch of one log record. Thus the watcher only deals with batches.
func (s *syncService) watchStore(ctx *context.T) {
	defer s.pending.Done()

	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for !s.isClosed() {
		select {
		case <-ticker.C:
			if s.isClosed() {
				break
			}
			s.processStoreUpdates(ctx)

		case <-s.closed:
			break
		}
	}

	vlog.VI(1).Info("sync: watchStore: channel closed, stop watching and exit")
}

// processStoreUpdates fetches updates from all databases and processes them.
// To maintain fairness among databases, it processes one batch update from
// each database, in a round-robin manner, until there are no further updates
// from any database.
func (s *syncService) processStoreUpdates(ctx *context.T) {
	for {
		total, active := 0, 0
		s.forEachDatabaseStore(ctx, func(appName, dbName string, st *watchable.Store) bool {
			if s.processDatabase(ctx, appName, dbName, st) {
				active++
			}
			total++
			return false
		})

		vlog.VI(2).Infof("sync: processStoreUpdates: %d/%d databases had updates", active, total)
		if active == 0 {
			break
		}
	}
}

// processDatabase fetches from the given database at most one new batch update
// (transaction) and processes it.  The one-batch limit prevents one database
// from starving others.  A batch is stored as a contiguous set of log records
// ending with one record having the "continued" flag set to false.  The call
// returns true if a new batch update was processed.
func (s *syncService) processDatabase(ctx *context.T, appName, dbName string, st store.Store) bool {
	s.thLock.Lock()
	defer s.thLock.Unlock()

	vlog.VI(2).Infof("sync: processDatabase: begin: %s, %s", appName, dbName)
	defer vlog.VI(2).Infof("sync: processDatabase: end: %s, %s", appName, dbName)

	resMark, err := getResMark(ctx, st)
	if err != nil {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			vlog.Errorf("sync: processDatabase: %s, %s: cannot get resMark: %v", appName, dbName, err)
			return false
		}
		resMark = watchable.MakeResumeMarker(0)
	}

	// Initialize Database sync state if needed.
	s.initSyncStateInMem(ctx, appName, dbName, "")

	// Get a batch of watch log entries, if any, after this resume marker.
	logs, nextResmark, err := watchable.ReadBatchFromLog(st, resMark)
	if err != nil {
		// An error here (scan stream cancelled) is possible when the
		// watcher is in the middle of processing a database and the
		// app/db is detroyed. Hence, we just ignore this database and
		// proceed.
		vlog.Errorf("sync: processDatabase: %s, %s: cannot get watch log batch: %v", appName, dbName, verror.DebugString(err))
		return false
	}
	if logs != nil {
		s.processWatchLogBatch(ctx, appName, dbName, st, logs, nextResmark)
		return true
	}
	return false
}

// processWatchLogBatch parses the given batch of watch log records, updates the
// watchable syncgroup prefixes, uses the prefixes to filter the batch to the
// subset of syncable records, and transactionally applies these updates to the
// sync metadata (DAG & log records) and updates the watch resume marker.
func (s *syncService) processWatchLogBatch(ctx *context.T, appName, dbName string, st store.Store, logs []*watchable.LogEntry, resMark watch.ResumeMarker) {
	if len(logs) == 0 {
		return
	}

	if processDbStateChangeLogRecord(ctx, s, st, appName, dbName, logs[0], resMark) {
		// A batch containing DbStateChange will not have any more records.
		// This batch is done processing.
		return
	}

	// If the first log entry is a syncgroup prefix operation, then this is
	// a syncgroup snapshot and not an application batch.  In this case,
	// handle the syncgroup prefix changes by updating the watch prefixes
	// and exclude the first entry from the batch.  Also inform the batch
	// processing below to not assign it a batch ID in the DAG.
	appBatch := true
	sgop := processSyncgroupLogRecord(appName, dbName, logs[0])
	if sgop != nil {
		appBatch = false
		logs = logs[1:]
	}

	// Filter out the log entries for keys not part of any syncgroup.
	// Ignore as well log entries made by sync (echo suppression).
	totalCount := uint64(len(logs))
	appdb := appDbName(appName, dbName)

	i := 0
	for _, entry := range logs {
		if !entry.FromSync && syncable(appdb, entry) {
			logs[i] = entry
			i++
		}
	}
	logs = logs[:i]
	vlog.VI(3).Infof("sync: processWatchLogBatch: %s, %s: sg snap %t, syncable %d, total %d",
		appName, dbName, !appBatch, len(logs), totalCount)

	// Transactional processing of the batch: convert these syncable log
	// records to a batch of sync log records, filling their parent versions
	// from the DAG head nodes.
	batch := make([]*LocalLogRec, len(logs))
	err := store.RunInTransaction(st, func(tx store.Transaction) error {
		i := 0
		for _, entry := range logs {
			if rec, err := convertLogRecord(ctx, tx, entry); err != nil {
				return err
			} else if rec != nil {
				batch[i] = rec
				i++
			}
		}
		batch = batch[:i]

		if err := s.processBatch(ctx, appName, dbName, batch, appBatch, totalCount, tx); err != nil {
			return err
		}

		if !appBatch {
			if err := setSyncgroupWatchable(ctx, tx, sgop); err != nil {
				return err
			}
		}

		return setResMark(ctx, tx, resMark)
	})

	if err != nil {
		// TODO(rdaoud): quarantine this app database for other errors.
		// There may be an error here if the database is recently
		// destroyed. Ignore the error and continue to another database.
		vlog.Errorf("sync: processWatchLogBatch: %s, %s: watcher cannot process batch: %v", appName, dbName, err)
		return
	}

	// Extract blob refs from batch values and update blob metadata.
	// TODO(rdaoud): the core of this step, extracting blob refs from the
	// app data, is idempotent and should be done before the transaction
	// above, which updated the resume marker.  If Syncbase crashes here it
	// does not re-do this blob ref processing at restart and the metadata
	// becomes lower quality.  Unfortunately, the log conversion must happen
	// inside the transaction because it accesses DAG information that must
	// be in the Tx read-set for optimistic locking.  The TODO is to split
	// the conversion into two phases, one non-transactional that happens
	// first outside the transaction, followed by blob ref processing also
	// outside the transaction (idempotent), then inside the transaction
	// patch-up the log records in a 2nd phase.
	if err = s.processWatchBlobRefs(ctx, appdb, st, batch); err != nil {
		// There may be an error here if the database is recently
		// destroyed. Ignore the error and continue to another database.
		vlog.Errorf("sync: processWatchLogBatch:: %s, %s: watcher cannot process blob refs: %v", appName, dbName, err)
		return
	}
}

// processBatch applies a single batch of changes (object mutations) received
// from watching a particular Database.
func (s *syncService) processBatch(ctx *context.T, appName, dbName string, batch []*LocalLogRec, appBatch bool, totalCount uint64, tx store.Transaction) error {
	count := uint64(len(batch))
	if count == 0 {
		return nil
	}

	// If an application batch has more than one mutation, start a batch for it.
	batchId := NoBatchId
	if appBatch && totalCount > 1 {
		batchId = s.startBatch(ctx, tx, batchId)
		if batchId == NoBatchId {
			return verror.New(verror.ErrInternal, ctx, "failed to generate batch ID")
		}
	}

	gen, pos := s.reserveGenAndPosInDbLog(ctx, appName, dbName, "", count)

	vlog.VI(3).Infof("sync: processBatch: %s, %s: len %d, total %d, btid %x, gen %d, pos %d",
		appName, dbName, count, totalCount, batchId, gen, pos)

	for _, rec := range batch {
		// Update the log record. Portions of the record Metadata must
		// already be filled.
		rec.Metadata.Id = s.id
		rec.Metadata.Gen = gen
		rec.Metadata.RecType = interfaces.NodeRec

		rec.Metadata.BatchId = batchId
		rec.Metadata.BatchCount = totalCount

		rec.Shell = false
		rec.Pos = pos

		gen++
		pos++

		if err := s.processLocalLogRec(ctx, tx, rec); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
	}

	// End the batch if any.
	if batchId != NoBatchId {
		if err := s.endBatch(ctx, tx, batchId, totalCount); err != nil {
			return err
		}
	}

	return nil
}

// processLocalLogRec processes a local log record by adding to the Database and
// suitably updating the DAG metadata.
func (s *syncService) processLocalLogRec(ctx *context.T, tx store.Transaction, rec *LocalLogRec) error {
	// Insert the new log record into the log.
	if err := putLogRec(ctx, tx, logDataPrefix, rec); err != nil {
		return err
	}

	m := &rec.Metadata
	logKey := logRecKey(logDataPrefix, m.Id, m.Gen)

	// Insert the new log record into dag.
	if err := s.addNode(ctx, tx, m.ObjId, m.CurVers, logKey, m.Delete, rec.Shell, m.Parents, m.BatchId, nil); err != nil {
		return err
	}

	// Move the head.
	return moveHead(ctx, tx, m.ObjId, m.CurVers)
}

// processWatchBlobRefs extracts blob refs from the data values of the updates
// received in the watch batch and updates the blob-to-syncgroup metadata.
func (s *syncService) processWatchBlobRefs(ctx *context.T, appdb string, st store.Store, batch []*LocalLogRec) error {
	if len(batch) == 0 {
		return nil
	}

	sgPfxs := watchPrefixes[appdb]
	if len(sgPfxs) == 0 {
		return verror.New(verror.ErrInternal, ctx, "processWatchBlobRefs: no sg prefixes in db", appdb)
	}

	for _, rec := range batch {
		m := &rec.Metadata
		if m.Delete {
			continue
		}

		buf, err := watchable.GetAtVersion(ctx, st, []byte(m.ObjId), nil, []byte(m.CurVers))
		if err != nil {
			return err
		}

		if err = s.processBlobRefs(ctx, appdb, st, s.name, true, sgPfxs, nil, m, buf); err != nil {
			return err
		}
	}
	return nil
}

// addWatchPrefixSyncgroup adds a syncgroup prefix-to-ID mapping for an app
// database in the watch prefix cache.
func addWatchPrefixSyncgroup(appName, dbName, prefix string, gid interfaces.GroupId) {
	name := appDbName(appName, dbName)
	if pfxs := watchPrefixes[name]; pfxs != nil {
		if sgs := pfxs[prefix]; sgs != nil {
			sgs[gid] = struct{}{}
		} else {
			pfxs[prefix] = sgSet{gid: struct{}{}}
		}
	} else {
		watchPrefixes[name] = sgPrefixes{prefix: sgSet{gid: struct{}{}}}
	}
}

// rmWatchPrefixSyncgroup removes a syncgroup prefix-to-ID mapping for an app
// database in the watch prefix cache.
func rmWatchPrefixSyncgroup(appName, dbName, prefix string, gid interfaces.GroupId) {
	name := appDbName(appName, dbName)
	if pfxs := watchPrefixes[name]; pfxs != nil {
		if sgs := pfxs[prefix]; sgs != nil {
			delete(sgs, gid)
			if len(sgs) == 0 {
				delete(pfxs, prefix)
				if len(pfxs) == 0 {
					delete(watchPrefixes, name)
				}
			}
		}
	}
}

// setSyncgroupWatchable sets the local watchable state of the syncgroup.
func setSyncgroupWatchable(ctx *context.T, tx store.Transaction, sgop *sbwatchable.SyncgroupOp) error {
	state, err := getSGIdEntry(ctx, tx, sgop.SgId)
	if err != nil {
		return err
	}
	state.Watched = !sgop.Remove

	return setSGIdEntry(ctx, tx, sgop.SgId, state)
}

// convertLogRecord converts a store log entry to a sync log record.  It fills
// the previous object version (parent) by fetching its current DAG head if it
// has one.  For a delete, it generates a new object version because the store
// does not version a deletion.
// TODO(rdaoud): change Syncbase to store and version a deleted object to
// simplify the store-to-sync interaction.  A deleted key would still have a
// version and its value entry would encode the "deleted" flag, either in the
// key or probably in a value wrapper that would contain other metadata.
func convertLogRecord(ctx *context.T, tx store.Transaction, logEnt *watchable.LogEntry) (*LocalLogRec, error) {
	var rec *LocalLogRec
	timestamp := logEnt.CommitTimestamp

	var op interface{}
	if err := logEnt.Op.ToValue(&op); err != nil {
		return nil, err
	}
	switch op := op.(type) {
	case *watchable.GetOp:
		// TODO(rdaoud): save read-set in sync.

	case *watchable.ScanOp:
		// TODO(rdaoud): save scan-set in sync.

	case *watchable.PutOp:
		rec = newLocalLogRec(ctx, tx, op.Key, op.Version, false, timestamp)

	case *sbwatchable.SyncSnapshotOp:
		// Create records for object versions not already in the DAG.
		// Duplicates can appear here in cases of nested syncgroups or
		// peer syncgroups.
		if ok, err := hasNode(ctx, tx, string(op.Key), string(op.Version)); err != nil {
			return nil, err
		} else if !ok {
			rec = newLocalLogRec(ctx, tx, op.Key, op.Version, false, timestamp)
		}

	case *watchable.DeleteOp:
		rec = newLocalLogRec(ctx, tx, op.Key, watchable.NewVersion(), true, timestamp)

	case *sbwatchable.SyncgroupOp:
		vlog.Errorf("sync: convertLogRecord: watch LogEntry for syncgroup should not be converted: %v", logEnt)
		return nil, verror.New(verror.ErrInternal, ctx, "cannot convert a watch log OpSyncgroup entry")

	default:
		vlog.Errorf("sync: convertLogRecord: invalid watch LogEntry: %v", logEnt)
		return nil, verror.New(verror.ErrInternal, ctx, "cannot convert unknown watch log entry")
	}

	return rec, nil
}

// newLocalLogRec creates a local sync log record given its information: key,
// version, deletion flag, and timestamp.  It retrieves the current DAG head
// for the key (if one exists) to use as its parent (previous) version.
func newLocalLogRec(ctx *context.T, tx store.Transaction, key, version []byte, deleted bool, timestamp int64) *LocalLogRec {
	rec := LocalLogRec{}
	oid := string(key)

	rec.Metadata.ObjId = oid
	rec.Metadata.CurVers = string(version)
	rec.Metadata.Delete = deleted
	if head, err := getHead(ctx, tx, oid); err == nil {
		rec.Metadata.Parents = []string{head}
	} else if deleted || (verror.ErrorID(err) != verror.ErrNoExist.ID) {
		vlog.Fatalf("sync: newLocalLogRec: cannot getHead to convert log record for %s: %v", oid, err)
	}
	rec.Metadata.UpdTime = unixNanoToTime(timestamp)
	return &rec
}

// processDbStateChangeLogRecord checks if the log entry is a
// DbStateChangeRequest and if so, it executes the state change request
// appropriately.
// TODO(razvanm): change the return type to error.
func processDbStateChangeLogRecord(ctx *context.T, s *syncService, st store.Store, appName, dbName string, logEnt *watchable.LogEntry, resMark watch.ResumeMarker) bool {
	var op interface{}
	if err := logEnt.Op.ToValue(&op); err != nil {
		vlog.Fatalf("sync: processDbStateChangeLogRecord: %s, %s: bad VOM: %v", appName, dbName, err)
	}
	switch op := op.(type) {
	case *sbwatchable.DbStateChangeRequestOp:
		dbStateChangeRequest := op
		vlog.VI(1).Infof("sync: processDbStateChangeLogRecord: found a dbState change log record with state %#v", dbStateChangeRequest)
		isPaused := false
		if err := store.RunInTransaction(st, func(tx store.Transaction) error {
			switch dbStateChangeRequest.RequestType {
			case sbwatchable.StateChangePauseSync:
				vlog.VI(1).Infof("sync: processDbStateChangeLogRecord: PauseSync request found. Pausing sync.")
				isPaused = true
			case sbwatchable.StateChangeResumeSync:
				vlog.VI(1).Infof("sync: processDbStateChangeLogRecord: ResumeSync request found. Resuming sync.")
				isPaused = false
			default:
				return fmt.Errorf("Unexpected DbStateChangeRequest found: %#v", dbStateChangeRequest)
			}
			// Update isPaused state in db.
			if err := s.updateDbPauseSt(ctx, tx, appName, dbName, isPaused); err != nil {
				return err
			}
			return setResMark(ctx, tx, resMark)
		}); err != nil {
			// TODO(rdaoud): don't crash, quarantine this app database.
			vlog.Fatalf("sync: processDbStateChangeLogRecord: %s, %s: watcher failed to reset dbState bits: %v", appName, dbName, err)
		}
		// Update isPaused state in cache.
		s.updateInMemoryPauseSt(ctx, appName, dbName, isPaused)
		return true

	default:
		return false
	}
}

// processSyncgroupLogRecord checks if the log entry is a syncgroup update and,
// if it is, updates the watch prefixes for the app database and returns a
// syncgroup operation.  Otherwise it returns nil with no other changes.
// TODO(razvanm): change the return to also include an error.
func processSyncgroupLogRecord(appName, dbName string, logEnt *watchable.LogEntry) *sbwatchable.SyncgroupOp {
	var op interface{}
	if err := logEnt.Op.ToValue(&op); err != nil {
		vlog.Fatalf("sync: processSyncgroupLogRecord: %s, %s: bad VOM: %v", appName, dbName, err)
	}
	switch op := op.(type) {
	case *sbwatchable.SyncgroupOp:
		gid, remove := op.SgId, op.Remove
		for _, prefix := range op.Prefixes {
			if remove {
				rmWatchPrefixSyncgroup(appName, dbName, prefix, gid)
			} else {
				addWatchPrefixSyncgroup(appName, dbName, prefix, gid)
			}
		}
		vlog.VI(3).Infof("sync: processSyncgroupLogRecord: %s, %s: gid %d, remove %t, prefixes: %q",
			appName, dbName, gid, remove, op.Prefixes)
		return op

	default:
		return nil
	}
}

// syncable returns true if the given log entry falls within the scope of a
// syncgroup prefix for the given app database, and thus should be synced.
// It is used to pre-filter the batch of log entries before sync processing.
// TODO(razvanm): change the return type to error.
func syncable(appdb string, logEnt *watchable.LogEntry) bool {
	var key string
	var op interface{}
	if err := logEnt.Op.ToValue(&op); err != nil {
		vlog.Fatalf("sync: syncable: %s: bad VOM: %v", appdb, err)
	}
	switch op := op.(type) {
	case *watchable.PutOp:
		key = string(op.Key)
	case *watchable.DeleteOp:
		key = string(op.Key)
	case *sbwatchable.SyncSnapshotOp:
		key = string(op.Key)
	default:
		return false
	}

	// The key starts with one of the store's reserved prefixes for managed
	// namespaces (e.g. "r" for row, "c" for collection perms). Remove that
	// prefix before comparing it with the syncgroup prefixes which are defined
	// by the application.
	key = common.StripFirstKeyPartOrDie(key)

	for prefix := range watchPrefixes[appdb] {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// resMarkKey returns the key used to access the watcher resume marker.
func resMarkKey() string {
	return common.JoinKeyParts(common.SyncPrefix, "w", "rm")
}

// setResMark stores the watcher resume marker for a database.
func setResMark(ctx *context.T, tx store.Transaction, resMark watch.ResumeMarker) error {
	return store.Put(ctx, tx, resMarkKey(), resMark)
}

// getResMark retrieves the watcher resume marker for a database.
func getResMark(ctx *context.T, st store.StoreReader) (watch.ResumeMarker, error) {
	var resMark watch.ResumeMarker
	key := resMarkKey()
	if err := store.Get(ctx, st, key, &resMark); err != nil {
		return nil, err
	}
	return resMark, nil
}
