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
	"v.io/v23/vdl"
	"v.io/v23/verror"
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
	if bytes.Equal(resumeMarker, []byte("now")) || len(resumeMarker) == 0 {
		var err error
		if resumeMarker, err = watchable.GetResumeMarker(d.st); err != nil {
			return err
		}
		if len(req.ResumeMarker) == 0 {
			// TODO(rogulenko): Fetch the initial state.
			return verror.NewErrNotImplemented(ctx)
		}
	}
	t := tableReq{
		name: table,
		d:    d,
	}
	return t.watchUpdates(ctx, call, prefix, resumeMarker)
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

	sender := call.SendStream()
	for {
		// Drain the log queue.
		for {
			logs, nextResumeMarker, err := watchable.ReadBatchFromLog(t.d.st, resumeMarker)
			if err != nil {
				return err
			}
			if logs == nil {
				// No new log records available now.
				break
			}
			resumeMarker = nextResumeMarker
			changes, err := t.processLogBatch(ctx, call, prefix, logs)
			if err != nil {
				return err
			}
			if changes == nil {
				// All batch changes are filtered out.
				continue
			}
			changes[len(changes)-1].ResumeMarker = resumeMarker
			for _, change := range changes {
				if err := sender.Send(change); err != nil {
					return err
				}
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

// processLogBatch converts []*watchable.LogEntry to []watch.Change, filtering
// out unnecessary or inaccessible log records.
func (t *tableReq) processLogBatch(ctx *context.T, call rpc.ServerCall, prefix string, logs []*watchable.LogEntry) ([]watch.Change, error) {
	sn := t.d.st.NewSnapshot()
	defer sn.Abort()
	var changes []watch.Change
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
				return nil, err
			}
			continue
		}
		change := watch.Change{
			Name:      naming.Join(table, row),
			Continued: true,
		}
		switch op := logEntry.Op.(type) {
		case watchable.OpPut:
			rowValue, err := watchable.GetAtVersion(ctx, sn, op.Value.Key, nil, op.Value.Version)
			if err != nil {
				return nil, err
			}
			change.State = watch.Exists
			change.Value = vdl.ValueOf(wire.StoreChange{
				Value:    rowValue,
				FromSync: logEntry.FromSync,
			})
		case watchable.OpDelete:
			change.State = watch.DoesNotExist
			change.Value = vdl.ValueOf(wire.StoreChange{
				FromSync: logEntry.FromSync,
			})
		}
		changes = append(changes, change)
	}
	if len(changes) > 0 {
		changes[len(changes)-1].Continued = false
	}
	return changes, nil
}
