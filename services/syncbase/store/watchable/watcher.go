// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package watchable

import (
	"fmt"
	"strconv"
	"sync"

	"v.io/v23/services/watch"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
)

// watcher maintains a set of watch clients receiving on update channels. When
// watcher is notified of a change in the store, it sends a value to all client
// channels that do not already have a value pending. This is done by a separate
// goroutine (watcherLoop) to move it off the broadcastUpdates() critical path.
type watcher struct {
	// Channel used by broadcastUpdates() to notify watcherLoop. When watcher is
	// closed, updater is closed.
	updater chan struct{}

	// Protects the fields below.
	mu sync.RWMutex
	// Currently registered clients, notified by watcherLoop via their channels.
	// When watcher is closed, all clients are stopped (and their channels closed)
	// with ErrAborted and clients is set to nil.
	clients map[*Client]struct{}
	// Sequence number pointing to the log start. Kept in sync with the value
	// persisted in the store under logStartSeqKey(). The log is a contiguous,
	// possibly empty, sequence of log entries beginning from logStart; log
	// entries before logStart may be partially garbage collected. logStart
	// will never move past an active watcher's seq.
	logStart uint64
}

func newWatcher(logStart uint64) *watcher {
	ret := &watcher{
		updater:  make(chan struct{}, 1),
		clients:  make(map[*Client]struct{}),
		logStart: logStart,
	}
	go ret.watcherLoop()
	return ret
}

// close closes the watcher. Idempotent.
func (w *watcher) close() {
	w.mu.Lock()
	if w.clients != nil {
		// Stop all clients and close their channels.
		for c := range w.clients {
			c.stop(verror.NewErrAborted(nil))
		}
		// Set clients to nil to mark watcher as closed.
		w.clients = nil
		// Close updater to notify watcherLoop to exit.
		closeAndDrain(w.updater)
	}
	w.mu.Unlock()
}

// broadcastUpdates notifies the watcher of an update. The watcher loop will
// propagate the notification to watch clients.
func (w *watcher) broadcastUpdates() {
	w.mu.RLock()
	if w.clients != nil {
		ping(w.updater)
	} else {
		vlog.Error("broadcastUpdates() called on a closed watcher")
	}
	w.mu.RUnlock()
}

// watcherLoop implements the goroutine that waits for updates and notifies any
// waiting clients.
func (w *watcher) watcherLoop() {
	for {
		// If updater has been closed, exit.
		if _, ok := <-w.updater; !ok {
			return
		}
		w.mu.RLock()
		for c := range w.clients { // safe for w.clients == nil
			ping(c.update)
		}
		w.mu.RUnlock()
	}
}

// ping writes a signal to a buffered notification channel. If a notification
// is already pending, it is a no-op.
func ping(c chan<- struct{}) {
	select {
	case c <- struct{}{}: // sent notification
	default: // already has notification pending
	}
}

// closeAndDrain closes a buffered notification channel and drains the buffer
// so that receivers see the closed state sooner.
func closeAndDrain(c chan struct{}) {
	close(c)
	for _, ok := <-c; ok; _, ok = <-c {
		// no-op
	}
}

// updateLogStartSeq - see UpdateLogStart.
func (w *watcher) updateLogStartSeq(st *Store, syncSeq uint64) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.clients == nil {
		return 0, verror.New(verror.ErrAborted, nil, "watcher closed")
	}
	// New log start is the minimum of all watch client seqs - the persistent sync
	// watcher and all ephemeral client watchers.
	lsSeq := syncSeq
	for c := range w.clients {
		if seq := c.getPrevSeq(); lsSeq > seq {
			lsSeq = seq
		}
	}
	if lsSeq < w.logStart {
		// New log start is earlier than the previous log start. This should never
		// happen since it means at least one watch client is incorrectly reading
		// log entries released to garbage collection.
		return 0, verror.New(verror.ErrInternal, nil, "watcher or sync seq less than log start")
	}
	if err := putLogStartSeq(st, lsSeq); err != nil {
		return 0, err
	}
	w.logStart = lsSeq
	return lsSeq, nil
}

// watchUpdates - see WatchUpdates.
func (w *watcher) watchUpdates(seq uint64) (_ *Client, cancel func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.clients == nil {
		// watcher is closed. Return stopped Client.
		return newStoppedClient(verror.NewErrAborted(nil)), func() {}
	}
	if seq < w.logStart {
		// Log start has moved past seq, so entries between seq and log start have
		// potentially been garbage collected. Return stopped Client.
		return newStoppedClient(verror.New(watch.ErrUnknownResumeMarker, nil, MakeResumeMarker(seq))), func() {}
	}
	// Register and return client.
	c := newClient(seq)
	w.clients[c] = struct{}{}
	cancel = func() {
		w.mu.Lock()
		if _, ok := w.clients[c]; ok { // safe for w.clients == nil
			c.stop(verror.NewErrCanceled(nil))
			delete(w.clients, c)
		}
		w.mu.Unlock()
	}
	return c, cancel
}

// UpdateLogStart takes as input the resume marker of the sync watcher and
// returns the new log start, computed as the earliest resume marker of all
// active watchers including the sync watcher. The new log start is persisted
// before being returned, making it safe to garbage collect earlier log entries.
// syncMarker is assumed to monotonically increase, always remaining between the
// log start and end (inclusive).
func (st *Store) UpdateLogStart(syncMarker watch.ResumeMarker) (watch.ResumeMarker, error) {
	syncSeq, err := parseResumeMarker(string(syncMarker))
	if err != nil {
		return nil, err
	}
	if logEnd := st.getSeq(); syncSeq > logEnd {
		// Sync has moved past log end. This should never happen.
		return nil, verror.New(verror.ErrInternal, nil, "sync seq greater than log end")
	}
	lsSeq, err := st.watcher.updateLogStartSeq(st, syncSeq)
	return MakeResumeMarker(lsSeq), err
}

// WatchUpdates returns a Client which supports waiting for changes and
// iterating over the watch log starting from resumeMarker, as well as a
// cancel function which MUST be called to release watch resources. Returns
// a stopped Client if the resume marker is invalid or pointing to an
// already garbage collected segment of the log.
func (st *Store) WatchUpdates(resumeMarker watch.ResumeMarker) (_ *Client, cancel func()) {
	seq, err := parseResumeMarker(string(resumeMarker))
	if err != nil {
		// resumeMarker is invalid. Return stopped Client.
		return newStoppedClient(err), func() {}
	}
	if logEnd := st.getSeq(); seq > logEnd {
		// resumeMarker points past log end. Return stopped Client.
		return newStoppedClient(verror.New(watch.ErrUnknownResumeMarker, nil, resumeMarker)), func() {}
	}
	return st.watcher.watchUpdates(seq)
}

// Client encapsulates a channel used to notify watch clients of store updates
// and an iterator over the watch log.
type Client struct {
	// Channel used by watcherLoop to notify the client. When the client is
	// stopped, update is closed.
	update chan struct{}

	// Protects the fields below.
	mu sync.Mutex
	// Sequence number pointing to the start of the previously retrieved log
	// batch. Equal to nextSeq if the retrieved batch was empty.
	prevSeq uint64
	// Sequence number pointing to the start of the next log batch to retrieve.
	nextSeq uint64
	// When the client is stopped, err is set to the reason for stopping.
	err error
}

func newClient(seq uint64) *Client {
	return &Client{
		update:  make(chan struct{}, 1),
		prevSeq: seq,
		nextSeq: seq,
	}
}

func newStoppedClient(err error) *Client {
	c := newClient(0)
	c.stop(err)
	return c
}

// Wait returns the update channel that can be used to wait for new changes in
// the store. If the update channel is closed, the client is stopped and no more
// updates will happen. Otherwise, the channel will have a value available
// whenever the store has changed since the last receive on the channel.
func (c *Client) Wait() <-chan struct{} {
	return c.update
}

// NextBatchFromLog returns the next batch of watch log records (transaction)
// from the given database and the resume marker at the end of the batch. If
// there is no batch available, it returns a nil slice and the same resume
// marker as the previous NextBatchFromLog call. The returned log entries are
// guaranteed to point to existing data versions until either the client is
// stopped or NextBatchFromLog is called again. If the client is stopped,
// NextBatchFromLog returns the same error as Err.
func (c *Client) NextBatchFromLog(st store.Store) ([]*LogEntry, watch.ResumeMarker, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, nil, c.err
	}
	batch, batchEndSeq, err := readBatchFromLog(st, c.nextSeq)
	if err != nil {
		// We cannot call stop() here since c.mu is locked. However, we checked
		// above that c.err is nil, so it is safe to set c.err anc close c.update.
		c.err = err
		closeAndDrain(c.update)
		return nil, nil, err
	}
	c.prevSeq = c.nextSeq
	c.nextSeq = batchEndSeq
	return batch, MakeResumeMarker(batchEndSeq), nil
}

// Err returns the error that caused the client to stop watching. If the error
// is nil, the client is active. Otherwise:
// * ErrCanceled - watch was canceled by the client.
// * ErrAborted - watcher was closed (store was closed, possibly destroyed).
// * ErrUnknownResumeMarker - watch was started with an invalid or too old resume marker.
// * other errors - NextBatchFromLog encountered an error.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// stop closes the client update channel and sets the error returned by Err.
// Idempotent (only the error from the first call to stop is kept).
func (c *Client) stop(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		closeAndDrain(c.update)
	}
	c.mu.Unlock()
}

func (c *Client) getPrevSeq() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prevSeq
}

// GetResumeMarker returns the ResumeMarker that points to the current end
// of the event log.
func GetResumeMarker(sntx store.SnapshotOrTransaction) (watch.ResumeMarker, error) {
	seq, err := getNextLogSeq(sntx)
	return watch.ResumeMarker(logEntryKey(seq)), err
}

// MakeResumeMarker converts a sequence number to the resume marker.
func MakeResumeMarker(seq uint64) watch.ResumeMarker {
	return watch.ResumeMarker(logEntryKey(seq))
}

func logEntryKey(seq uint64) string {
	// Note: MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	return common.JoinKeyParts(common.LogPrefix, fmt.Sprintf("%016x", seq))
}

// readBatchFromLog returns a batch of watch log records (a transaction) from
// the given database and the next sequence number at the end of the batch.
// Assumes that the log start is less than seq during its execution.
func readBatchFromLog(st store.Store, seq uint64) ([]*LogEntry, uint64, error) {
	_, scanLimit := common.ScanPrefixArgs(common.LogPrefix, "")
	scanStart := MakeResumeMarker(seq)
	endOfBatch := false

	// Use the store directly to scan these read-only log entries, no need to
	// create a snapshot since log entries are never overwritten and are not
	// deleted before the log start moves past them. Read and buffer a batch
	// before processing it.
	var logs []*LogEntry
	stream := st.Scan(scanStart, scanLimit)
	defer stream.Cancel()
	for stream.Advance() {
		seq++
		var logEnt LogEntry
		if err := vom.Decode(stream.Value(nil), &logEnt); err != nil {
			return nil, seq, err
		}

		logs = append(logs, &logEnt)

		// Stop if this is the end of the batch.
		if logEnt.Continued == false {
			endOfBatch = true
			break
		}
	}

	if !endOfBatch {
		if err := stream.Err(); err != nil {
			return nil, seq, err
		}
		if len(logs) > 0 {
			return nil, seq, verror.New(verror.ErrInternal, nil, fmt.Sprintf("end of batch not found after %d entries", len(logs)))
		}
		return nil, seq, nil
	}
	return logs, seq, nil
}

func parseResumeMarker(resumeMarker string) (uint64, error) {
	// See logEntryKey() for the structure of a resume marker key.
	parts := common.SplitNKeyParts(resumeMarker, 2)
	if len(parts) != 2 {
		return 0, verror.New(watch.ErrUnknownResumeMarker, nil, resumeMarker)
	}
	seq, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return 0, verror.New(watch.ErrUnknownResumeMarker, nil, resumeMarker)
	}
	return seq, nil
}

func logStartSeqKey() string {
	return common.JoinKeyParts(common.LogMarkerPrefix, "st")
}

func getLogStartSeq(st store.StoreReader) (uint64, error) {
	var seq uint64
	if err := store.Get(nil, st, logStartSeqKey(), &seq); err != nil {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return 0, err
		}
		return 0, nil
	}
	return seq, nil
}

func putLogStartSeq(st *Store, seq uint64) error {
	// The log start key must not be managed because getLogStartSeq is called both
	// on the wrapped store and on the watchable store.
	if st.managesKey([]byte(logStartSeqKey())) {
		panic("log start key must not be managed")
	}
	// We put directly into the wrapped store to avoid using a watchable store
	// transaction, which may cause a deadlock by calling broadcastUpdates().
	return store.Put(nil, st.ist, logStartSeqKey(), seq)
}

// logEntryExists returns true iff the log contains an entry with the given
// sequence number.
func logEntryExists(st store.StoreReader, seq uint64) (bool, error) {
	_, err := st.Get([]byte(logEntryKey(seq)), nil)
	if err != nil && verror.ErrorID(err) != store.ErrUnknownKey.ID {
		return false, err
	}
	return err == nil, nil
}

// getNextLogSeq returns the next sequence number to be used for a new commit.
// NOTE: This function assumes that all sequence numbers in the log represent
// some range [start, limit] without gaps. It also assumes that the log is not
// changing (by appending entries or moving the log start) during its execution.
// Therefore, it should only be called on a snapshot or a store with no active
// potential writers (e.g. when opening the store). Furthermore, it assumes that
// common.LogMarkerPrefix and common.LogPrefix are not managed prefixes.
// TODO(ivanpi): Consider replacing this function with persisted log end,
// similar to how log start is handled.
func getNextLogSeq(st store.StoreReader) (uint64, error) {
	// Read initial value for seq.
	// TODO(sadovsky): Consider using a bigger seq.
	seq, err := getLogStartSeq(st)
	if err != nil {
		return 0, err
	}
	// Handle empty log case.
	if ok, err := logEntryExists(st, seq); err != nil {
		return 0, err
	} else if !ok {
		return 0, nil
	}
	var step uint64 = 1
	// Suppose the actual value we are looking for is S. First, we estimate the
	// range for S. We find seq, step: seq < S <= seq + step.
	for {
		if ok, err := logEntryExists(st, seq+step); err != nil {
			return 0, err
		} else if !ok {
			break
		}
		seq += step
		step *= 2
	}
	// Next we keep the seq < S <= seq + step invariant, reducing step to 1.
	for step > 1 {
		step /= 2
		if ok, err := logEntryExists(st, seq+step); err != nil {
			return 0, err
		} else if ok {
			seq += step
		}
	}
	// Now seq < S <= seq + 1, thus S = seq + 1.
	return seq + 1, nil
}
