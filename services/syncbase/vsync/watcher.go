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
	"strings"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
)

var (
	// watchPollInterval is the duration between consecutive watch polling
	// events across all app databases.  Every watch event loops across all
	// app databases and fetches from each one at most one batch update
	// (transaction) to process.
	// TODO(rdaoud): add a channel between store and watch to get change
	// notifications instead of using a polling solution.
	watchPollInterval = 100 * time.Millisecond

	// watchPrefixes is an in-memory cache of SyncGroup prefixes for each
	// app database.  It is filled at startup from persisted SyncGroup data
	// and updated at runtime when SyncGroups are joined or left.  It is
	// not guarded by a mutex because only the watcher goroutine uses it
	// beyond the startup phase (before any sync goroutines are started).
	// The map keys are the appdb names (globally unique).
	watchPrefixes = make(map[string]sgPrefixes)
)

// sgPrefixes tracks SyncGroup prefixes being synced in a database and their
// counts.
type sgPrefixes map[string]uint32

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

	for {
		select {
		case <-s.closed:
			vlog.VI(1).Info("sync: watchStore: channel closed, stop watching and exit")
			return

		case <-ticker.C:
			s.processStoreUpdates(ctx)
		}
	}
}

// processStoreUpdates fetches updates from all databases and processes them.
// To maintain fairness among databases, it processes one batch update from
// each database, in a round-robin manner, until there are no further updates
// from any database.
func (s *syncService) processStoreUpdates(ctx *context.T) {
	for {
		total, active := 0, 0
		s.forEachDatabaseStore(ctx, func(appName, dbName string, st store.Store) bool {
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
		resMark = ""
	}

	// Initialize Database sync state if needed.
	s.initDbSyncStateInMem(ctx, appName, dbName)

	// Get a batch of watch log entries, if any, after this resume marker.
	if logs, nextResmark := getWatchLogBatch(ctx, appName, dbName, st, resMark); logs != nil {
		s.processWatchLogBatch(ctx, appName, dbName, st, logs, nextResmark)
		return true
	}
	return false
}

// processWatchLogBatch parses the given batch of watch log records, updates the
// watchable SyncGroup prefixes, uses the prefixes to filter the batch to the
// subset of syncable records, and transactionally applies these updates to the
// sync metadata (DAG & log records) and updates the watch resume marker.
func (s *syncService) processWatchLogBatch(ctx *context.T, appName, dbName string, st store.Store, logs []*watchable.LogEntry, resMark string) {
	if len(logs) == 0 {
		return
	}

	// If the first log entry is a SyncGroup prefix operation, then this is
	// a SyncGroup snapshot and not an application batch.  In this case,
	// handle the SyncGroup prefix changes by updating the watch prefixes
	// and exclude the first entry from the batch.  Also inform the batch
	// processing below to not assign it a batch ID in the DAG.
	appBatch := true
	if processSyncGroupLogRecord(appName, dbName, logs[0]) {
		appBatch = false
		logs = logs[1:]
	}

	// Filter out the log entries for keys not part of any SyncGroup.
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
	err := store.RunInTransaction(st, func(tx store.StoreReadWriter) error {
		batch := make([]*localLogRec, 0, len(logs))
		for _, entry := range logs {
			if rec, err := convertLogRecord(ctx, tx, entry); err != nil {
				return err
			} else if rec != nil {
				batch = append(batch, rec)
			}
		}

		if err := s.processBatch(ctx, appName, dbName, batch, appBatch, totalCount, tx); err != nil {
			return err
		}
		return setResMark(ctx, tx, resMark)
	})

	if err != nil {
		// TODO(rdaoud): don't crash, quarantine this app database.
		vlog.Fatalf("sync: processWatchLogBatch:: %s, %s: watcher cannot process batch: %v", appName, dbName, err)
	}
}

// processBatch applies a single batch of changes (object mutations) received
// from watching a particular Database.
func (s *syncService) processBatch(ctx *context.T, appName, dbName string, batch []*localLogRec, appBatch bool, totalCount uint64, tx store.StoreReadWriter) error {
	_ = tx.(store.Transaction)

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

	gen, pos := s.reserveGenAndPosInDbLog(ctx, appName, dbName, count)

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
func (s *syncService) processLocalLogRec(ctx *context.T, tx store.StoreReadWriter, rec *localLogRec) error {
	// Insert the new log record into the log.
	if err := putLogRec(ctx, tx, rec); err != nil {
		return err
	}

	m := rec.Metadata
	logKey := logRecKey(m.Id, m.Gen)

	// Insert the new log record into dag.
	if err := s.addNode(ctx, tx, m.ObjId, m.CurVers, logKey, m.Delete, m.Parents, m.BatchId, nil); err != nil {
		return err
	}

	// Move the head.
	return moveHead(ctx, tx, m.ObjId, m.CurVers)
}

// incrWatchPrefix increments (or sets) a SyncGroup prefix for an app database
// in the watch prefix cache.
func incrWatchPrefix(appName, dbName, prefix string) {
	name := appDbName(appName, dbName)
	if pfxs := watchPrefixes[name]; pfxs != nil {
		pfxs[prefix]++ // it auto-initializes a non-existent prefix
	} else {
		watchPrefixes[name] = sgPrefixes{prefix: 1}
	}
}

// decrWatchPrefix decrements (or unsets) a SyncGroup prefix for an app database
// in the watch prefix cache.
func decrWatchPrefix(appName, dbName, prefix string) {
	name := appDbName(appName, dbName)
	if pfxs := watchPrefixes[name]; pfxs != nil {
		if pfxs[prefix] > 1 {
			pfxs[prefix]--
		} else if len(pfxs) > 1 {
			delete(pfxs, prefix)
		} else {
			delete(watchPrefixes, name)
		}
	}
}

// dbLogScanArgs determines the arguments needed to start a new scan from a
// given resume marker (last log entry read).  An empty resume marker is used
// to begin the scan from the start of the log.
func dbLogScanArgs(resMark string) ([]byte, []byte) {
	start, limit := util.ScanPrefixArgs(util.LogPrefix, "")
	if resMark != "" {
		// To start just after the current resume marker, augment it by
		// appending an extra byte at the end.  Use byte value zero to
		// use the lowest value possible.  This works because resume
		// markers have a fixed length and are sorted lexicographically.
		// By creationg a fake longer resume marker that falls between
		// real resume markers, the next scan will start right after
		// where the previous one stopped without missing data.
		start = append([]byte(resMark), 0)
	}
	return start, limit
}

// getWatchLogBatch returns a batch of watch log records (a transaction) from
// the given database and the new resume marker at the end of the batch.
func getWatchLogBatch(ctx *context.T, appName, dbName string, st store.Store, resMark string) ([]*watchable.LogEntry, string) {
	scanStart, scanLimit := dbLogScanArgs(resMark)
	endOfBatch := false
	var newResmark string

	// Use the store directly to scan these read-only log entries, no need
	// to create a snapshot since they are never overwritten.  Read and
	// buffer a batch before processing it.
	var logs []*watchable.LogEntry
	stream := st.Scan(scanStart, scanLimit)
	for stream.Advance() {
		logKey := string(stream.Key(nil))
		var logEnt watchable.LogEntry
		if vom.Decode(stream.Value(nil), &logEnt) != nil {
			vlog.Fatalf("sync: getWatchLogBatch: %s, %s: invalid watch LogEntry %s: %v",
				appName, dbName, logKey, stream.Value(nil))
		}

		logs = append(logs, &logEnt)

		// Stop if this is the end of the batch.
		if logEnt.Continued == false {
			newResmark = logKey
			endOfBatch = true
			break
		}
	}

	if err := stream.Err(); err != nil {
		vlog.Errorf("sync: getWatchLogBatch: %s, %s: scan stream error: %v", appName, dbName, err)
		return nil, resMark
	}
	if !endOfBatch {
		if len(logs) > 0 {
			vlog.Fatalf("sync: getWatchLogBatch: %s, %s: end of batch not found after %d entries",
				appName, dbName, len(logs))
		}
		return nil, resMark
	}
	return logs, newResmark
}

// convertLogRecord converts a store log entry to a sync log record.  It fills
// the previous object version (parent) by fetching its current DAG head if it
// has one.  For a delete, it generates a new object version because the store
// does not version a deletion.
// TODO(rdaoud): change Syncbase to store and version a deleted object to
// simplify the store-to-sync interaction.  A deleted key would still have a
// version and its value entry would encode the "deleted" flag, either in the
// key or probably in a value wrapper that would contain other metadata.
func convertLogRecord(ctx *context.T, tx store.StoreReadWriter, logEnt *watchable.LogEntry) (*localLogRec, error) {
	_ = tx.(store.Transaction)
	var rec *localLogRec
	timestamp := logEnt.CommitTimestamp

	switch op := logEnt.Op.(type) {
	case watchable.OpGet:
		// TODO(rdaoud): save read-set in sync.

	case watchable.OpScan:
		// TODO(rdaoud): save scan-set in sync.

	case watchable.OpPut:
		rec = newLocalLogRec(ctx, tx, op.Value.Key, op.Value.Version, false, timestamp)

	case watchable.OpSyncSnapshot:
		// Create records for object versions not already in the DAG.
		// Duplicates can appear here in cases of nested SyncGroups or
		// peer SyncGroups.
		if ok, err := hasNode(ctx, tx, string(op.Value.Key), string(op.Value.Version)); err != nil {
			return nil, err
		} else if !ok {
			rec = newLocalLogRec(ctx, tx, op.Value.Key, op.Value.Version, false, timestamp)
		}

	case watchable.OpDelete:
		rec = newLocalLogRec(ctx, tx, op.Value.Key, watchable.NewVersion(), true, timestamp)

	case watchable.OpSyncGroup:
		vlog.Errorf("sync: convertLogRecord: watch LogEntry for SyncGroup should not be converted: %v", logEnt)
		return nil, verror.New(verror.ErrInternal, ctx, "cannot convert a watch log OpSyncGroup entry")

	default:
		vlog.Errorf("sync: convertLogRecord: invalid watch LogEntry: %v", logEnt)
		return nil, verror.New(verror.ErrInternal, ctx, "cannot convert unknown watch log entry")
	}

	return rec, nil
}

// newLocalLogRec creates a local sync log record given its information: key,
// version, deletion flag, and timestamp.  It retrieves the current DAG head
// for the key (if one exists) to use as its parent (previous) version.
func newLocalLogRec(ctx *context.T, tx store.StoreReadWriter, key, version []byte, deleted bool, timestamp int64) *localLogRec {
	_ = tx.(store.Transaction)

	rec := localLogRec{}
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

// processSyncGroupLogRecord checks if the log entry is a SyncGroup update and,
// if it is, updates the watch prefixes for the app database and returns true.
// Otherwise it returns false with no other changes.
func processSyncGroupLogRecord(appName, dbName string, logEnt *watchable.LogEntry) bool {
	switch op := logEnt.Op.(type) {
	case watchable.OpSyncGroup:
		remove := op.Value.Remove
		for _, prefix := range op.Value.Prefixes {
			if remove {
				decrWatchPrefix(appName, dbName, prefix)
			} else {
				incrWatchPrefix(appName, dbName, prefix)
			}
		}
		vlog.VI(3).Infof("sync: processSyncGroupLogRecord: %s, %s: remove %t, prefixes: %q",
			appName, dbName, remove, op.Value.Prefixes)
		return true

	default:
		return false
	}
}

// syncable returns true if the given log entry falls within the scope of a
// SyncGroup prefix for the given app database, and thus should be synced.
// It is used to pre-filter the batch of log entries before sync processing.
func syncable(appdb string, logEnt *watchable.LogEntry) bool {
	var key string
	switch op := logEnt.Op.(type) {
	case watchable.OpPut:
		key = string(op.Value.Key)
	case watchable.OpDelete:
		key = string(op.Value.Key)
	case watchable.OpSyncSnapshot:
		key = string(op.Value.Key)
	default:
		return false
	}

	// The key starts with one of the store's reserved prefixes for managed
	// namespaced (e.g. $row or $perm).  Remove that prefix before comparing
	// it with the SyncGroup prefixes which are defined by the application.
	parts := util.SplitKeyParts(key)
	if len(parts) < 2 {
		vlog.Fatalf("sync: syncable: %s: invalid entry key %s: %v", appdb, key, logEnt)
	}
	key = util.JoinKeyParts(parts[1:]...)

	for prefix := range watchPrefixes[appdb] {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// resMarkKey returns the key used to access the watcher resume marker.
func resMarkKey() string {
	return util.JoinKeyParts(util.SyncPrefix, "w", "rm")
}

// setResMark stores the watcher resume marker for a database.
func setResMark(ctx *context.T, tx store.StoreReadWriter, resMark string) error {
	_ = tx.(store.Transaction)
	return util.Put(ctx, tx, resMarkKey(), resMark)
}

// getResMark retrieves the watcher resume marker for a database.
func getResMark(ctx *context.T, st store.StoreReader) (string, error) {
	var resMark string
	key := resMarkKey()
	if err := util.Get(ctx, st, key, &resMark); err != nil {
		return NoVersion, err
	}
	return resMark, nil
}
