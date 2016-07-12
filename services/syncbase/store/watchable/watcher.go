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

	// Protects the clients map.
	mu sync.RWMutex
	// Currently registered clients, notified by watcherLoop via their channels.
	// When watcher is closed, all clients are stopped (and their channels closed)
	// with ErrAborted and clients is set to nil.
	clients map[*Client]struct{}
}

func newWatcher() *watcher {
	ret := &watcher{
		updater: make(chan struct{}, 1),
		clients: make(map[*Client]struct{}),
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

// watchUpdates - see WatchUpdates.
func (w *watcher) watchUpdates(seq uint64) (_ *Client, cancel func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.clients == nil {
		// watcher is closed. Return stopped Client.
		return newStoppedClient(verror.NewErrAborted(nil)), func() {}
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

// WatchUpdates returns a Client which supports waiting for changes and
// iterating over the watch log starting from resumeMarker, as well as a
// cancel function which MUST be called to release watch resources.
func (st *Store) WatchUpdates(resumeMarker watch.ResumeMarker) (_ *Client, cancel func()) {
	seq, err := parseResumeMarker(string(resumeMarker))
	if err != nil {
		// resumeMarker is invalid. Return stopped Client.
		return newStoppedClient(err), func() {}
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
// * ErrUnknownResumeMarker - watch was started with an invalid resume marker.
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

// GetResumeMarker returns the ResumeMarker that points to the current end
// of the event log.
func GetResumeMarker(st store.StoreReader) (watch.ResumeMarker, error) {
	seq, err := getNextLogSeq(st)
	return watch.ResumeMarker(logEntryKey(seq)), err
}

// MakeResumeMarker converts a sequence number to the resume marker.
func MakeResumeMarker(seq uint64) watch.ResumeMarker {
	return watch.ResumeMarker(logEntryKey(seq))
}

func logEntryKey(seq uint64) string {
	// Note: MaxUint64 is 0xffffffffffffffff.
	// TODO(sadovsky): Use a more space-efficient lexicographic number encoding.
	return join(common.LogPrefix, fmt.Sprintf("%016x", seq))
}

// readBatchFromLog returns a batch of watch log records (a transaction) from
// the given database and the next sequence number at the end of the batch.
func readBatchFromLog(st store.Store, seq uint64) ([]*LogEntry, uint64, error) {
	_, scanLimit := common.ScanPrefixArgs(common.LogPrefix, "")
	scanStart := MakeResumeMarker(seq)
	endOfBatch := false

	// Use the store directly to scan these read-only log entries, no need
	// to create a snapshot since they are never overwritten.  Read and
	// buffer a batch before processing it.
	var logs []*LogEntry
	stream := st.Scan(scanStart, scanLimit)
	for stream.Advance() {
		seq++
		var logEnt LogEntry
		if err := vom.Decode(stream.Value(nil), &logEnt); err != nil {
			stream.Cancel()
			return nil, seq, err
		}

		logs = append(logs, &logEnt)

		// Stop if this is the end of the batch.
		if logEnt.Continued == false {
			endOfBatch = true
			stream.Cancel()
			break
		}
	}

	if !endOfBatch {
		if err := stream.Err(); err != nil {
			return nil, seq, err
		}
		if len(logs) > 0 {
			vlog.Fatalf("end of batch not found after %d entries", len(logs))
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
// NOTE: this function assumes that all sequence numbers in the log represent
// some range [start, limit] without gaps.
func getNextLogSeq(st store.StoreReader) (uint64, error) {
	// Determine initial value for seq.
	// TODO(sadovsky): Consider using a bigger seq.

	// Find the beginning of the log.
	it := st.Scan(common.ScanPrefixArgs(common.LogPrefix, ""))
	if !it.Advance() {
		return 0, nil
	}
	defer it.Cancel()
	if it.Err() != nil {
		return 0, it.Err()
	}
	seq, err := parseResumeMarker(string(it.Key(nil)))
	if err != nil {
		return 0, err
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
