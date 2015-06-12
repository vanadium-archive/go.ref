// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/store"

	"v.io/v23/context"
	"v.io/v23/verror"
)

// When applications update objects in the local Store, the sync
// watcher thread learns about them asynchronously via the "watch"
// stream of object mutations. In turn, this sync watcher thread
// updates the DAG and log records to track the object change
// histories.

// watchStore processes updates obtained by watching the store.
func (s *syncService) watchStore() {
	defer s.pending.Done()
}

// TODO(hpucha): This is a skeleton only to drive the change for log.

// processBatch applies a single batch of changes (object mutations) received
// from watching a particular Database.
func (s *syncService) processBatch(ctx *context.T, appName, dbName string, batch []*localLogRec, tx store.StoreReadWriter) error {
	_ = tx.(store.Transaction)

	count := uint64(len(batch))
	if count == 0 {
		return nil
	}

	// If the batch has more than one mutation, start a batch for it.
	batchId := NoBatchId
	if count > 1 {
		batchId = s.startBatch(ctx, tx, batchId)
		if batchId == NoBatchId {
			return verror.New(verror.ErrInternal, ctx, "failed to generate batch ID")
		}
	}

	gen, pos := s.reserveGenAndPosInDbLog(ctx, appName, dbName, count)

	for _, rec := range batch {

		// Update the log record. Portions of the record Metadata must
		// already be filled.
		rec.Metadata.Id = s.id
		rec.Metadata.Gen = gen
		rec.Metadata.RecType = interfaces.NodeRec

		rec.Metadata.BatchId = batchId
		rec.Metadata.BatchCount = count

		rec.Pos = pos

		gen++
		pos++

		if err := s.processLocalLogRec(ctx, tx, rec); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
	}

	// End the batch if any.
	if batchId != NoBatchId {
		if err := s.endBatch(ctx, tx, batchId, count); err != nil {
			return err
		}
	}

	return nil
}

// processLogRec processes a local log record by adding to the Database and
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
