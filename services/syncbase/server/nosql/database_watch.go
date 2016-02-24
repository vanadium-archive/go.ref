// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"bytes"
	"strings"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/services/watch"
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
)

// GetResumeMarker implements the wire.DatabaseWatcher interface.
func (d *databaseReq) GetResumeMarker(ctx *context.T, call rpc.ServerCall) (watch.ResumeMarker, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return watchable.GetResumeMarker(d.batchReader())
	} else {
		return watchable.GetResumeMarker(d.st)
	}
}

// WatchGlob implements the wire.DatabaseWatcher interface.
func (d *databaseReq) WatchGlob(ctx *context.T, call watch.GlobWatcherWatchGlobServerCall, req watch.GlobRequest) error {
	// TODO(rogulenko): Check permissions here and in other methods.
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	// Parse the pattern.
	if !strings.HasSuffix(req.Pattern, "*") {
		return verror.New(verror.ErrBadArg, ctx, req.Pattern)
	}
	table, prefix, err := pubutil.ParseTableRowPair(ctx, strings.TrimSuffix(req.Pattern, "*"))
	if err != nil {
		return err
	}
	// Get the resume marker and fetch the initial state if necessary.
	resumeMarker := req.ResumeMarker
	t := tableReq{
		name: table,
		d:    d,
	}
	if bytes.Equal(resumeMarker, []byte("now")) {
		var err error
		if resumeMarker, err = watchable.GetResumeMarker(d.st); err != nil {
			return err
		}
	}
	if len(resumeMarker) == 0 {
		var err error
		if resumeMarker, err = t.scanInitialState(ctx, call, prefix); err != nil {
			return err
		}
	}
	return t.watchUpdates(ctx, call, prefix, resumeMarker)
}

// scanInitialState sends the initial state of the watched prefix as one batch.
// TODO(ivanpi): Send dummy update for empty prefix to be compatible with
// v.io/v23/services/watch.
func (t *tableReq) scanInitialState(ctx *context.T, call watch.GlobWatcherWatchGlobServerCall, prefix string) (watch.ResumeMarker, error) {
	sntx := t.d.st.NewSnapshot()
	defer sntx.Abort()
	// Get resume marker inside snapshot.
	resumeMarker, err := watchable.GetResumeMarker(sntx)
	if err != nil {
		return nil, err
	}
	// Scan initial state, sending accessible rows as single batch.
	it := sntx.Scan(util.ScanPrefixArgs(util.JoinKeyParts(util.RowPrefix, t.name), prefix))
	sender := &watchBatchSender{
		send: call.SendStream().Send,
	}
	key, value := []byte{}, []byte{}
	for it.Advance() {
		key, value = it.Key(key), it.Value(value)
		// Check perms.
		// See comment in util/constants.go for why we use SplitNKeyParts.
		parts := util.SplitNKeyParts(string(key), 3)
		externalKey := parts[2]
		if _, err := t.checkAccess(ctx, call, sntx, externalKey); err != nil {
			// TODO(ivanpi): Inaccessible rows are skipped. Figure out how to signal
			// this to caller.
			if verror.ErrorID(err) == verror.ErrNoAccess.ID {
				continue
			} else {
				it.Cancel()
				return nil, err
			}
		}
		if err := sender.addChange(
			naming.Join(t.name, externalKey),
			watch.Exists,
			vom.RawBytesOf(wire.StoreChange{
				Value: value,
				// Note: FromSync cannot be reconstructed from scan.
				FromSync: false,
			})); err != nil {
			it.Cancel()
			return nil, err
		}
	}
	if err := it.Err(); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	// Finalize initial state batch.
	if err := sender.finishBatch(resumeMarker); err != nil {
		return nil, err
	}
	return resumeMarker, nil
}

// watchUpdates waits for database updates and sends them to the client.
// This function does two steps in a for loop:
// - scan through the watch log until the end, sending all updates to the client
// - wait for one of two signals: new updates available or the call is canceled.
// The 'new updates' signal is sent by a worker goroutine that translates a
// condition variable signal to a Go channel. The worker goroutine waits on the
// condition variable for changes. Whenever the state changes, the worker sends
// a signal through the Go channel.
func (t *tableReq) watchUpdates(ctx *context.T, call watch.GlobWatcherWatchGlobServerCall, prefix string, resumeMarker watch.ResumeMarker) error {
	// The Go channel to send notifications from the worker to the main
	// goroutine.
	hasUpdates := make(chan struct{})
	// The Go channel to signal the worker to stop. The worker might block
	// on the condition variable, but we don't want the main goroutine
	// to wait for the worker to stop, so we create a buffered channel.
	cancelWorker := make(chan struct{}, 1)
	defer close(cancelWorker)
	go func() {
		waitForChange := watchable.WatchUpdates(t.d.st)
		var state, newState uint64 = 1, 1
		for {
			// Wait until the state changes or the main function returns.
			for newState == state {
				select {
				case <-cancelWorker:
					return
				default:
				}
				newState = waitForChange(state)
			}
			// Update the current state to the new value and sends a signal to
			// the main goroutine.
			state = newState
			if state == 0 {
				close(hasUpdates)
				return
			}
			// cancelWorker is closed as soons as the main function returns.
			select {
			case hasUpdates <- struct{}{}:
			case <-cancelWorker:
				return
			}
		}
	}()

	sender := &watchBatchSender{
		send: call.SendStream().Send,
	}
	for {
		// Drain the log queue.
		for {
			// TODO(ivanpi): Switch to streaming log batch entries? Since sync and
			// conflict resolution merge batches, very large batches may not be
			// unrealistic. However, sync currently also processes an entire batch at
			// a time, and would need to be updated as well.
			logs, nextResumeMarker, err := watchable.ReadBatchFromLog(t.d.st, resumeMarker)
			if err != nil {
				return err
			}
			if logs == nil {
				// No new log records available at this time.
				break
			}
			resumeMarker = nextResumeMarker
			if err := t.processLogBatch(ctx, call, prefix, logs, sender); err != nil {
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
func (t *tableReq) processLogBatch(ctx *context.T, call watch.GlobWatcherWatchGlobServerCall, prefix string, logs []*watchable.LogEntry, sender *watchBatchSender) error {
	sn := t.d.st.NewSnapshot()
	defer sn.Abort()
	for _, logEntry := range logs {
		var opKey string
		switch op := logEntry.Op.(type) {
		case watchable.OpPut:
			opKey = string(op.Value.Key)
		case watchable.OpDelete:
			opKey = string(op.Value.Key)
		default:
			continue
		}
		// TODO(rogulenko): Currently we only process rows, i.e. keys of the form
		// <RowPrefix>:xxx:yyy. Consider processing other keys.
		if !util.IsRowKey(opKey) {
			continue
		}
		table, row := util.ParseTableAndRowOrDie(opKey)
		// Filter out unnecessary rows and rows that we can't access.
		if table != t.name || !strings.HasPrefix(row, prefix) {
			continue
		}
		if _, err := t.checkAccess(ctx, call, sn, row); err != nil {
			if verror.ErrorID(err) != verror.ErrNoAccess.ID {
				return err
			}
			continue
		}
		switch op := logEntry.Op.(type) {
		case watchable.OpPut:
			rowValue, err := watchable.GetAtVersion(ctx, sn, op.Value.Key, nil, op.Value.Version)
			if err != nil {
				return err
			}
			if err := sender.addChange(naming.Join(table, row),
				watch.Exists,
				vom.RawBytesOf(wire.StoreChange{
					Value:    rowValue,
					FromSync: logEntry.FromSync,
				})); err != nil {
				return err
			}
		case watchable.OpDelete:
			if err := sender.addChange(naming.Join(table, row),
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
