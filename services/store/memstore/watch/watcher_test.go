package watch

import (
	"io"
	"sync"
	"testing"
	"time"

	"veyron/services/store/memstore"
	watchtesting "veyron/services/store/memstore/watch/testing"
	"veyron/services/store/raw"

	"veyron2/storage"
)

func TestWatch(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Put /
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	commit(t, tr)

	post1 := st.Snapshot().Find(id1).Version

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Check that watch detects the changes in the first transaction.
	cb, err := ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes := cb.Changes
	change := changes[0]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id1, storage.NoVersion, post1, true, "val1", empty)

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Check that watch detects the changes in the second transaction.

	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	change = changes[1]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id1, pre1, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", empty)
}

func TestWatchCancellation(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Commit a transaction.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	commit(t, tr)

	// Check that watch processed the first transaction.
	if _, err := ws.Recv(); err != nil {
		t.Error("Expected a change.")
	}

	// Cancel the watch request.
	ws.Cancel()
	// Give watch some time to process the cancellation.
	time.Sleep(time.Second)

	// Commit a second transaction.
	tr = memstore.NewTransaction()
	put(t, st, tr, "/a", "val2")
	commit(t, tr)

	// Check that watch did not processed the second transaction.
	if _, err := ws.Recv(); err != watchtesting.ErrStreamClosed {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check that io.EOF was returned.
	if err := ws.Finish(); err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestWatchClosed(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	var once sync.Once
	defer once.Do(cleanup)

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Commit a transaction.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	commit(t, tr)

	// Check that watch processed the first transaction.
	if _, err := ws.Recv(); err != nil {
		t.Error("Expected a change.")
	}

	// Close the watcher, check that io.EOF was returned.
	once.Do(cleanup)
	if err := ws.Finish(); err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestStateResumeMarker(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Put /
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	commit(t, tr)

	post11 := st.Snapshot().Find(id1).Version

	if err := st.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Re-create a new store. This should compress the log, creating an initial
	// state containing / and /a.
	st, cleanup = openStore(t, dbName)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	pre21 := post11
	post21 := st.Snapshot().Find(id1).Version
	post22 := st.Snapshot().Find(id2).Version

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Retrieve the resume marker for the initial state.
	cb, err := ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes := cb.Changes
	change := changes[0]
	resumeMarker1 := change.ResumeMarker

	// Cancel the watch request.
	ws.Cancel()
	ws.Finish()

	// Start a watch request after the initial state.
	req = raw.Request{ResumeMarker: resumeMarker1}
	ws = watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Check that watch detects the changes in the state and the transaction.
	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id1, storage.NoVersion, post11, true, "val1", empty)

	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	change = changes[1]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id1, pre21, post21, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post22, false, "val2", empty)
}

func TestTransactionResumeMarker(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Put /
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	commit(t, tr)

	post11 := st.Snapshot().Find(id1).Version

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	pre21 := post11
	post21 := st.Snapshot().Find(id1).Version
	post22 := st.Snapshot().Find(id2).Version

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Retrieve the resume marker for the first transaction.
	cb, err := ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes := cb.Changes
	change := changes[0]
	resumeMarker1 := change.ResumeMarker

	// Cancel the watch request.
	ws.Cancel()
	ws.Finish()

	// Start a watch request after the first transaction.
	req = raw.Request{ResumeMarker: resumeMarker1}
	ws = watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Check that watch detects the changes in the first and second transaction.
	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id1, storage.NoVersion, post11, true, "val1", empty)

	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	change = changes[1]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	resumeMarker2 := change.ResumeMarker
	expectExists(t, changes, id1, pre21, post21, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post22, false, "val2", empty)

	// Cancel the watch request.
	ws.Cancel()
	ws.Finish()

	// Start a watch request at the second transaction.
	req = raw.Request{ResumeMarker: resumeMarker2}
	ws = watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Check that watch detects the changes in the second transaction.
	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	change = changes[1]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id1, pre21, post21, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post22, false, "val2", empty)
}

func TestNowResumeMarker(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Put /
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	commit(t, tr)

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	post22 := st.Snapshot().Find(id2).Version

	// Start a watch request with the "now" resume marker.
	req := raw.Request{ResumeMarker: nowResumeMarker}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// Give watch some time to pick "now".
	time.Sleep(time.Second)

	// Put /a/b
	tr = memstore.NewTransaction()
	id3 := put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	pre32 := post22
	post32 := st.Snapshot().Find(id2).Version
	post33 := st.Snapshot().Find(id3).Version

	// Check that watch announces that the initial state was skipped.
	cb, err := ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes := cb.Changes
	change := changes[0]
	expectInitialStateSkipped(t, change)

	// Check that watch detects the changes in the third transaction.
	cb, err = ws.Recv()
	if err != nil {
		t.Error("Recv() failed: %v", err)
	}
	changes = cb.Changes
	change = changes[0]
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	change = changes[1]
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	expectExists(t, changes, id2, pre32, post32, false, "val2", dir("b", id3))
	expectExists(t, changes, id3, storage.NoVersion, post33, false, "val3", empty)
}

func TestUnknownResumeMarkers(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Put /
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	commit(t, tr)

	// Start a watch request with a resume marker that's too early.
	resumeMarker := timestampToResumeMarker(1)
	req := raw.Request{ResumeMarker: resumeMarker}
	ws := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// The resume marker should be unknown.
	if err := ws.Finish(); err != ErrUnknownResumeMarker {
		t.Errorf("Unexpected error: %v", err)
	}

	// Start a watch request with a resume marker that's too late.
	resumeMarker = timestampToResumeMarker(2 ^ 63 - 1)
	req = raw.Request{ResumeMarker: resumeMarker}
	ws = watchtesting.WatchRaw(rootPublicID, w.WatchRaw, req)

	// The resume marker should be unknown.
	if err := ws.Finish(); err != ErrUnknownResumeMarker {
		t.Errorf("Unexpected error: %v", err)
	}
}
