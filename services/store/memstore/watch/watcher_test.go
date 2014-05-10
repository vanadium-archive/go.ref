package watch

import (
	"io"
	"sync"
	"testing"
	"time"

	"veyron/services/store/memstore"
	"veyron2/services/watch"
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
	req := watch.Request{}
	wr := doWatch(w, rootCtx, req)

	// Check that watch detects the changes in the first transaction.
	changes := make([]watch.Change, 0, 0)
	change, ok := <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	changes = append(changes, change)
	expectExists(t, changes, id1, storage.NoVersion, post1, true, "val1", empty)

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Check that watch detects the changes in the second transaction.

	changes = make([]watch.Change, 0, 0)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	changes = append(changes, change)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	changes = append(changes, change)
	expectExists(t, changes, id1, pre1, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", empty)

	// Check that no errors were encountered.
	if err := wr.err; err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
}

func TestWatchCancellation(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	var once sync.Once
	defer once.Do(cleanup)

	// Start a watch request.
	req := watch.Request{}
	ctx := newCancellableContext()
	wr := doWatch(w, ctx, req)

	// Commit a transaction.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	commit(t, tr)

	// Check that watch processed the first transaction.
	if _, ok := <-wr.changes; !ok {
		t.Error("Expected a change.")
	}

	// Cancel the watch request.
	ctx.Cancel()
	// Give watch some time to process the cancellation.
	time.Sleep(time.Second)

	// Commit a second transaction.
	tr = memstore.NewTransaction()
	put(t, st, tr, "/a", "val2")
	commit(t, tr)

	// Check that watch did not processed the second transaction.
	if _, ok := <-wr.changes; ok {
		t.Error("Did not expect a change.")
	}

	// Close the watcher, check that io.EOF was returned.
	once.Do(cleanup)
	<-wr.changes
	if err := wr.err; err != io.EOF {
		t.Errorf("Unexpected error: %s", err)
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
	req := watch.Request{}
	wr := doWatch(w, rootCtx, req)

	// Commit a transaction.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	commit(t, tr)

	// Check that watch processed the first transaction.
	if _, ok := <-wr.changes; !ok {
		t.Error("Expected a change.")
	}

	// Close the watcher, check that io.EOF was returned.
	once.Do(cleanup)
	<-wr.changes
	if err := wr.err; err != io.EOF {
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

	post1 := st.Snapshot().Find(id1).Version

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

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Start a watch request.
	req := watch.Request{}
	wr := doWatch(w, rootCtx, req)

	// Retrieve the resume marker for the initial state.
	change, ok := <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	resumeMarker1 := change.ResumeMarker

	// Start a watch request after the initial state.
	req = watch.Request{ResumeMarker: resumeMarker1}
	wr = doWatch(w, rootCtx, req)

	// Check that watch detects the changes the transaction.
	changes := make([]watch.Change, 0, 0)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	changes = append(changes, change)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	changes = append(changes, change)
	expectExists(t, changes, id1, pre1, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", empty)

	// Check that no errors were encountered.
	if err := wr.err; err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
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

	post1 := st.Snapshot().Find(id1).Version

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Start a watch request.
	req := watch.Request{}
	wr := doWatch(w, rootCtx, req)

	// Retrieve the resume marker for the first transaction.
	change, ok := <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	resumeMarker1 := change.ResumeMarker

	// Start a watch request after the first transaction.
	req = watch.Request{ResumeMarker: resumeMarker1}
	wr = doWatch(w, rootCtx, req)

	// Check that watch detects the changes in the second transaction.
	changes := make([]watch.Change, 0, 0)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	changes = append(changes, change)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	changes = append(changes, change)
	expectExists(t, changes, id1, pre1, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", empty)

	// Check that no errors were encountered.
	if err := wr.err; err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
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
	id1 := put(t, st, tr, "/", "val1")
	commit(t, tr)

	post1 := st.Snapshot().Find(id1).Version

	// Start a watch request with the "now" resume marker.
	req := watch.Request{ResumeMarker: nowResumeMarker}
	wr := doWatch(w, rootCtx, req)

	// Check that watch announces that the initial state was skipped.
	change, ok := <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	expectInitialStateSkipped(t, change)

	// Put /a
	tr = memstore.NewTransaction()
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Check that watch detects the changes in the second transaction.
	changes := make([]watch.Change, 0, 0)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if !change.Continued {
		t.Error("Expected change to continue the transaction")
	}
	changes = append(changes, change)
	change, ok = <-wr.changes
	if !ok {
		t.Error("Expected a change.")
	}
	if change.Continued {
		t.Error("Expected change to be the last in this transaction")
	}
	changes = append(changes, change)
	expectExists(t, changes, id1, pre1, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", empty)

	// Check that no errors were encountered.
	if err := wr.err; err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

}

func TestUnknownResumeMarkers(t *testing.T) {
	// Create a new store.
	dbName, _, cleanup := createStore(t)
	defer cleanup()

	// Create a new watcher.
	w, cleanup := createWatcher(t, dbName)
	defer cleanup()

	// Start a watch request with a resume marker that is too long.
	resumeMarker := make([]byte, 9)
	req := watch.Request{ResumeMarker: resumeMarker}
	wr := doWatch(w, rootCtx, req)

	// The resume marker should be too long.
	<-wr.changes
	if err := wr.err; err != ErrUnknownResumeMarker {
		t.Errorf("Unexpected error: %v", err)
	}
}
