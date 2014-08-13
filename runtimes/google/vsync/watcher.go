package vsync

// Veyron Sync hook to the Store Watch API.  When applications update objects
// in the local Veyron Store, Sync learns about these via the Watch API stream
// of object mutations.  In turn, this Sync watcher thread updates the DAG and
// ILog records to track the object change histories.

import (
	"fmt"
	"io"
	"time"

	"veyron/services/store/raw"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/services/watch"
	"veyron2/vlog"
)

var (
	// watchRetryDelay is how long the watcher waits before calling the Watch() RPC again
	// after the previous call fails.
	watchRetryDelay = 2 * time.Second
	// streamRetryDelay is how long the watcher waits before creating a new Watch stream
	// after the previous stream ends.
	streamRetryDelay = 1 * time.Second
)

// syncWatcher contains the metadata for the Sync Watcher thread.
type syncWatcher struct {
	// syncd is a pointer to the Syncd instance owning this Watcher.
	syncd *syncd

	// curTx is the transaction ID of the latest transaction being processed by the Watcher.
	curTx TxID

	// curTxSyncTime is the timestamp of the latest transaction being processed by the Watcher.
	curTxSyncTime int64
}

// newWatcher creates a new Sync Watcher instance attached to the given Syncd instance.
func newWatcher(syncd *syncd) *syncWatcher {
	return &syncWatcher{syncd: syncd, curTx: NoTxID}
}

// watchStreamCanceler is a helper goroutine that cancels the watcher RPC when Syncd notifies
// its goroutines to exit by closing its internal channel.  This in turn unblocks the watcher
// enabling it to exit.  If the RPC fails, the watcher notifies the canceler to exit by
// closing a private "done" channel between them.
func (w *syncWatcher) watchStreamCanceler(stream watch.GlobWatcherWatchGlobCall, done chan struct{}) {
	select {
	case <-w.syncd.closed:
		vlog.VI(1).Info("watchStreamCanceler: Syncd channel closed, cancel stream and exit")
		stream.Cancel()
	case <-done:
		vlog.VI(1).Info("watchStreamCanceler: done, exit")
	}
}

// watchStore consumes change records obtained by watching the store
// for updates and applies them to the sync DBs.
func (w *syncWatcher) watchStore() {
	defer w.syncd.pending.Done()

	// If no Veyron store is configured, there is nothing to watch.
	if w.syncd.store == nil {
		vlog.VI(1).Info("watchStore: Veyron store not configured; skipping the watcher")
		return
	}

	// Get a Watch stream, process it, repeat till end-of-life.
	ctx := rt.R().NewContext()
	for {
		stream := w.getWatchStream(ctx)
		if stream == nil {
			return // Syncd is exiting.
		}

		// Spawn a goroutine to detect the Syncd "closed" channel and cancel the RPC stream
		// to unblock the watcher.  The "done" channel lets the watcher terminate the goroutine.
		done := make(chan struct{})
		go w.watchStreamCanceler(stream, done)

		// Process the stream of Watch updates until it closes (similar to "tail -f").
		w.processWatchStream(stream)

		if w.syncd.isSyncClosing() {
			return // Syncd is exiting.
		}

		stream.Finish()
		close(done)

		// TODO(rdaoud): we need a rate-limiter here in case the stream closes too quickly.
		// If the stream stays up long enough, no need to sleep before getting a new one.
		time.Sleep(streamRetryDelay)
	}
}

// getWatchStream() returns a Watch API stream and handles retries if the Watch() call fails.
// If the stream is nil, it means Syncd is exiting cleanly and the caller should terminate.
func (w *syncWatcher) getWatchStream(ctx context.T) watch.GlobWatcherWatchGlobCall {
	for {
		req := raw.Request{}
		if resmark := w.syncd.devtab.head.Resmark; resmark != nil {
			req.ResumeMarker = resmark
		}

		stream, err := w.syncd.store.Watch(ctx, req, veyron2.CallTimeout(ipc.NoTimeout))
		if err == nil {
			return stream
		}

		if w.syncd.isSyncClosing() {
			vlog.VI(1).Info("getWatchStream: exiting, Syncd closed its channel: ", err)
			return nil
		}

		vlog.VI(1).Info("getWatchStream: call to Watch() failed, retrying a bit later: ", err)
		time.Sleep(watchRetryDelay)
	}
}

// processWatchStream reads the stream of Watch updates and applies the object mutations.
// Ideally this call does not return as the stream should be un-ending (like "tail -f").
// If the stream is closed, distinguish between the cases of end-of-stream vs Syncd canceling
// the stream to trigger a clean exit.
func (w *syncWatcher) processWatchStream(call watch.GlobWatcherWatchGlobCall) {
	w.curTx = NoTxID
	stream := call.RecvStream()
	for stream.Advance() {
		changes := stream.Value()

		// Timestamp of these changes arriving at the Sync server.
		syncTime := time.Now().UnixNano()

		if err := w.processChanges(changes, syncTime); err != nil {
			// TODO(rdaoud): don't crash, instead add retry policies to attempt some degree of
			// self-healing from a data corruption where feasible, otherwise quarantine this device
			// from the cluster and stop Syncd to avoid propagating data corruptions.
			vlog.Fatal("processWatchStream:", err)
		}
	}

	err := stream.Err()
	if err == nil {
		err = io.EOF
	}
	if w.syncd.isSyncClosing() {
		vlog.VI(1).Info("processWatchStream: exiting, Syncd closed its channel: ", err)
	} else {
		vlog.VI(1).Info("processWatchStream: RPC stream error, re-issue Watch(): ", err)
	}
}

// processChanges applies the batch of changes (object mutations) received from the Watch API.
// The function grabs the write-lock to access the Log and DAG DBs.
func (w *syncWatcher) processChanges(changes watch.ChangeBatch, syncTime int64) error {
	w.syncd.lock.Lock()
	defer w.syncd.lock.Unlock()

	vlog.VI(1).Infof("processChanges:: ready to process changes")

	var lastResmark []byte
	for i := range changes.Changes {
		ch := &changes.Changes[i]
		mu, ok := ch.Value.(*raw.Mutation)
		if !ok {
			return fmt.Errorf("invalid change value, not a mutation: %#v", ch)
		}

		// Begin a new transaction if needed.
		if w.curTx == NoTxID && ch.Continued {
			w.curTx = w.syncd.dag.addNodeTxStart()
			w.curTxSyncTime = syncTime
		}

		time := syncTime
		if w.curTx != NoTxID {
			// All LogValues belonging to the same transaction get the same timestamp.
			time = w.curTxSyncTime
		}
		val := &LogValue{Mutation: *mu, SyncTime: time, Delete: ch.State == watch.DoesNotExist, Continued: ch.Continued}
		vlog.VI(2).Infof("processChanges:: processing record %v, Tx %v", val, w.curTx)
		if err := w.syncd.log.processWatchRecord(mu.ID, mu.Version, mu.PriorVersion, val, w.curTx); err != nil {
			return fmt.Errorf("cannot process mutation: %#v: %s", ch, err)
		}

		if !ch.Continued {
			lastResmark = ch.ResumeMarker
		}

		// End the previous transaction if any.
		if w.curTx != NoTxID && !ch.Continued {
			if err := w.syncd.dag.addNodeTxEnd(w.curTx); err != nil {
				return err
			}
			w.curTx = NoTxID
		}
	}

	// If the resume marker changed, update the device table.
	if lastResmark != nil {
		w.syncd.devtab.head.Resmark = lastResmark
	}

	return nil
}
