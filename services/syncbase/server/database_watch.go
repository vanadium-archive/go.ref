// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/services/watch"
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/filter"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/watchable"
)

// GetResumeMarker implements the wire.DatabaseWatcher interface.
func (d *database) GetResumeMarker(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (watch.ResumeMarker, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.id)
	}
	var res watch.ResumeMarker
	impl := func(sntx store.SnapshotOrTransaction) (err error) {
		res, err = watchable.GetResumeMarker(sntx)
		return err
	}
	if err := d.runWithExistingBatchOrNewSnapshot(ctx, bh, impl); err != nil {
		return nil, err
	}
	return res, nil
}

// WatchGlob implements the wire.DatabaseWatcher interface.
func (d *database) WatchGlob(ctx *context.T, call watch.GlobWatcherWatchGlobServerCall, req watch.GlobRequest) error {
	sender := &watchBatchSender{
		send: call.SendStream().Send,
	}
	gf, err := filter.NewGlobFilter(req.Pattern)
	if err != nil {
		return verror.New(verror.ErrBadArg, ctx, err)
	}
	return d.watchWithFilter(ctx, call, sender, req.ResumeMarker, gf)
}

// WatchPatterns implements the wire.DatabaseWatcher interface.
func (d *database) WatchPatterns(ctx *context.T, call wire.DatabaseWatcherWatchPatternsServerCall, resumeMarker watch.ResumeMarker, patterns []wire.CollectionRowPattern) error {
	sender := &watchBatchSender{
		send: call.SendStream().Send,
	}
	mpf, err := filter.NewMultiPatternFilter(patterns)
	if err != nil {
		return verror.New(verror.ErrBadArg, ctx, err)
	}
	return d.watchWithFilter(ctx, call, sender, resumeMarker, mpf)
}

// watchWithFilter sends the initial state (if necessary) and watch events,
// filtered using watchFilter, to the caller using sender.
func (d *database) watchWithFilter(ctx *context.T, call rpc.ServerCall, sender *watchBatchSender, resumeMarker watch.ResumeMarker, watchFilter filter.CollectionRowFilter) error {
	// TODO(ivanpi): Check permissions here and in other methods.
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	initImpl := func(sntx store.SnapshotOrTransaction) error {
		// TODO(ivanpi): Check permissions here.
		// Get the resume marker and fetch the initial state if necessary.
		if len(resumeMarker) == 0 {
			var err error
			if resumeMarker, err = watchable.GetResumeMarker(sntx); err != nil {
				return err
			}
			// Send initial state.
			if err = d.scanInitialState(ctx, call, sender, sntx, watchFilter); err != nil {
				return err
			}
		} else if bytes.Equal(resumeMarker, []byte("now")) {
			var err error
			// TODO(ivanpi): Add initial_state_skipped change.
			if resumeMarker, err = watchable.GetResumeMarker(sntx); err != nil {
				return err
			}
		}
		// Finalize initial state batch if necessary.
		return sender.finishBatch(resumeMarker)
	}
	if err := store.RunWithSnapshot(d.st, initImpl); err != nil {
		return err
	}
	return d.watchUpdates(ctx, call, sender, resumeMarker, watchFilter)
}

// scanInitialState sends the initial state of all matching and accessible
// collections and rows in the database. Checks access on collections, but
// not on database.
// TODO(ivanpi): Send dummy update for empty prefix to be compatible with
// v.io/v23/services/watch.
// TODO(ivanpi): Abstract out multi-scan for scan and possibly query support.
// TODO(ivanpi): Use watch pattern prefixes to optimize scan ranges.
func (d *database) scanInitialState(ctx *context.T, call rpc.ServerCall, sender *watchBatchSender, sntx store.SnapshotOrTransaction, watchFilter filter.CollectionRowFilter) error {
	// Scan matching and accessible collections.
	// TODO(ivanpi): Collection scan order not alphabetical.
	cxIt := sntx.Scan(common.ScanPrefixArgs(common.CollectionPermsPrefix, ""))
	defer cxIt.Cancel()
	cxKey := []byte{}
	for cxIt.Advance() {
		cxKey = cxIt.Key(cxKey)
		// See comment in util/constants.go for why we use SplitNKeyParts.
		// TODO(rdaoud,ivanpi): See hack in collection.go.
		cxParts := common.SplitNKeyParts(string(cxKey), 3)
		cxId, err := pubutil.DecodeId(cxParts[1])
		if err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		// Filter out unnecessary collections.
		if !watchFilter.CollectionMatches(cxId) {
			continue
		}
		c := &collectionReq{
			id: cxId,
			d:  d,
		}
		// Check permissions.
		// TODO(ivanpi): Collection scan already gets perms, optimize?
		if _, err := c.checkAccess(ctx, call, sntx); err != nil {
			if verror.ErrorID(err) != verror.ErrNoAccess.ID {
				return err
			}
			// TODO(ivanpi): Inaccessible collections are skipped. Figure out how to
			// signal this to caller.
			continue
		}
		// TODO(ivanpi): Send collection info.
		// Send matching rows.
		if err := c.scanInitialState(ctx, call, sender, sntx, watchFilter); err != nil {
			return err
		}
	}
	if err := cxIt.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// scanInitialState sends the initial state of all matching rows in the
// collection. Does not check access.
// TODO(ivanpi): Abstract out multi-scan for scan and possibly query support.
// TODO(ivanpi): Use watch pattern prefixes to optimize scan ranges.
func (c *collectionReq) scanInitialState(ctx *context.T, call rpc.ServerCall, sender *watchBatchSender, sntx store.SnapshotOrTransaction, watchFilter filter.CollectionRowFilter) error {
	// Scan matching rows.
	rIt := sntx.Scan(common.ScanPrefixArgs(common.JoinKeyParts(common.RowPrefix, c.stKeyPart()), ""))
	defer rIt.Cancel()
	key, value := []byte{}, []byte{}
	for rIt.Advance() {
		key, value = rIt.Key(key), rIt.Value(value)
		// See comment in util/constants.go for why we use SplitNKeyParts.
		parts := common.SplitNKeyParts(string(key), 3)
		externalKey := parts[2]
		// Filter out unnecessary rows.
		if !watchFilter.RowMatches(c.id, externalKey) {
			continue
		}
		// Send row.
		var valueAsRawBytes *vom.RawBytes
		if err := vom.Decode(value, &valueAsRawBytes); err != nil {
			return err
		}
		if err := sender.addChange(
			naming.Join(pubutil.EncodeId(c.id), externalKey),
			watch.Exists,
			vom.RawBytesOf(wire.StoreChange{
				Value: valueAsRawBytes,
				// Note: FromSync cannot be reconstructed from scan.
				FromSync: false,
			})); err != nil {
			return err
		}
	}
	if err := rIt.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// watchUpdates waits for database updates and sends them to the client.
// This function does two steps in a for loop:
// - scan through the watch log until the end, sending all updates to the client
// - wait for one of two signals: new updates available or the call is canceled
// The 'new updates' signal is sent by the watcher via a Go channel.
func (d *database) watchUpdates(ctx *context.T, call rpc.ServerCall, sender *watchBatchSender, resumeMarker watch.ResumeMarker, watchFilter filter.CollectionRowFilter) error {
	hasUpdates, cancelWatch := watchable.WatchUpdates(d.st)
	defer cancelWatch()
	for {
		// Drain the log queue.
		for {
			// TODO(ivanpi): Switch to streaming log batch entries? Since sync and
			// conflict resolution merge batches, very large batches may not be
			// unrealistic. However, sync currently also processes an entire batch at
			// a time, and would need to be updated as well.
			logs, nextResumeMarker, err := watchable.ReadBatchFromLog(d.st, resumeMarker)
			if err != nil {
				return err
			}
			if logs == nil {
				// No new log records available at this time.
				break
			}
			resumeMarker = nextResumeMarker
			if err := d.processLogBatch(ctx, call, sender, watchFilter, logs); err != nil {
				return err
			}
			if err := sender.finishBatch(resumeMarker); err != nil {
				return err
			}
		}
		// Wait for new updates or cancel.
		select {
		case _, ok := <-hasUpdates:
			if !ok {
				return verror.NewErrAborted(ctx)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// processLogBatch converts []*watchable.LogEntry to a watch.Change stream,
// filtering out unnecessary or inaccessible log records.
// Note: Since the governing ACL for each change is no longer tracked, the
// permissions check uses the ACLs in effect at the time processLogBatch is
// called.
func (d *database) processLogBatch(ctx *context.T, call rpc.ServerCall, sender *watchBatchSender, watchFilter filter.CollectionRowFilter, logs []*watchable.LogEntry) error {
	sn := d.st.NewSnapshot()
	defer sn.Abort()
	// TODO(ivanpi): Recheck database perms here and fail, or cache for collection
	// access checks.
	for _, logEntry := range logs {
		var opKey string
		var op interface{}
		if err := logEntry.Op.ToValue(&op); err != nil {
			return err
		}
		switch op := op.(type) {
		case *watchable.PutOp:
			opKey = string(op.Key)
		case *watchable.DeleteOp:
			opKey = string(op.Key)
		default:
			continue
		}
		// TODO(rogulenko): Currently we only process rows, i.e. keys of the form
		// <RowPrefix>:xxx:yyy. Consider processing other keys.
		if !common.IsRowKey(opKey) {
			continue
		}
		cxId, row := common.ParseRowKeyOrDie(opKey)
		// Filter out unnecessary rows.
		if !watchFilter.RowMatches(cxId, row) {
			continue
		}
		c := &collectionReq{
			id: cxId,
			d:  d,
		}
		// Filter out rows that we can't access.
		// TODO(ivanpi): Check only once per collection per batch.
		if _, err := c.checkAccess(ctx, call, sn); err != nil {
			if verror.ErrorID(err) != verror.ErrNoAccess.ID {
				return err
			}
			// TODO(ivanpi): Inaccessible rows are skipped. Figure out how to signal
			// this to caller.
			continue
		}
		switch op := op.(type) {
		case *watchable.PutOp:
			rowValue, err := watchable.GetAtVersion(ctx, sn, op.Key, nil, op.Version)
			if err != nil {
				return err
			}
			var rowValueAsRawBytes *vom.RawBytes
			if err := vom.Decode(rowValue, &rowValueAsRawBytes); err != nil {
				return err
			}
			if err := sender.addChange(naming.Join(pubutil.EncodeId(cxId), row),
				watch.Exists,
				vom.RawBytesOf(wire.StoreChange{
					Value:    rowValueAsRawBytes,
					FromSync: logEntry.FromSync,
				})); err != nil {
				return err
			}
		case *watchable.DeleteOp:
			if err := sender.addChange(naming.Join(pubutil.EncodeId(cxId), row),
				watch.DoesNotExist,
				vom.RawBytesOf(wire.StoreChange{
					FromSync: logEntry.FromSync,
				})); err != nil {
				return err
			}
		}
	}
	return nil
}

// watchBatchSender sends a sequence of watch changes forming a batch, delaying
// sends to allow setting the Continued flag on the last change.
type watchBatchSender struct {
	// Function for sending changes to the stream. Must be set.
	send func(item watch.Change) error

	// Change set by previous addChange, sent by next addChange or finishBatch.
	staged *watch.Change
}

// addChange sends the previously added change (if any) with Continued set to
// true and stages the new one to be sent by the next addChange or finishBatch.
func (w *watchBatchSender) addChange(name string, state int32, value *vom.RawBytes) error {
	// Send previously staged change now that we know the batch is continuing.
	if w.staged != nil {
		w.staged.Continued = true
		if err := w.send(*w.staged); err != nil {
			return err
		}
	}
	// Stage new change.
	w.staged = &watch.Change{
		Name:  name,
		State: state,
		Value: value,
	}
	return nil
}

// finishBatch sends the previously added change (if any) with Continued set to
// false, finishing the batch.
func (w *watchBatchSender) finishBatch(resumeMarker watch.ResumeMarker) error {
	// Send previously staged change as last in batch.
	if w.staged != nil {
		w.staged.Continued = false
		w.staged.ResumeMarker = resumeMarker
		if err := w.send(*w.staged); err != nil {
			return err
		}
	}
	// Clear staged change.
	w.staged = nil
	return nil
}
