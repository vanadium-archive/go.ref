package blackbox

import (
	"testing"

	"veyron/services/store/memstore"
	watchtesting "veyron/services/store/memstore/testing"
	"veyron/services/store/raw"
)

func TestSyncState(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := CreateStore(t, "vstore_source")
	defer cleanup()

	// Put /, /a, /a/b
	tr := memstore.NewTransaction()
	id1 := Put(t, st, tr, "/", "val1")
	id2 := Put(t, st, tr, "/a", "val2")
	Put(t, st, tr, "/a/b", "val3")
	Commit(t, tr)

	// Remove /a/b
	tr = memstore.NewTransaction()
	Remove(t, st, tr, "/a/b")
	Commit(t, tr)
	GC(t, st)

	if err := st.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Create a target store for integration testing.
	_, target, cleanup := CreateStore(t, "vstore_target")
	defer cleanup()

	// Re-create a new store. This should compress the log, creating an initial
	// state containing / and /a.
	st, cleanup = OpenStore(t, dbName)
	defer cleanup()

	// Create the watcher
	w, cleanup := OpenWatch(t, dbName)
	defer cleanup()
	// Create a sync request
	stream := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, raw.Request{})

	cb, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() failed: %v", err)
	}
	// Update target
	PutMutations(t, target, Mutations(cb.Changes))
	GC(t, target)

	// Expect that the target contains id1 and id2
	ExpectExists(t, target, id1)
	ExpectExists(t, target, id2)
}

func TestSyncTransaction(t *testing.T) {
	_, target, cleanup := CreateStore(t, "vstore_target")
	defer cleanup()

	dbName, st, cleanup := CreateStore(t, "vstore_source")
	defer cleanup()

	// Create the watcher
	w, cleanup := OpenWatch(t, dbName)
	defer cleanup()
	// Create a sync request
	stream := watchtesting.WatchRaw(rootPublicID, w.WatchRaw, raw.Request{})

	// First transaction, put /, /a, /a/b
	tr := memstore.NewTransaction()
	id1 := Put(t, st, tr, "/", "val1")
	id2 := Put(t, st, tr, "/a", "val2")
	id3 := Put(t, st, tr, "/a/b", "val3")
	Commit(t, tr)

	cb, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() failed: %v", err)
	}
	// Update target
	PutMutations(t, target, Mutations(cb.Changes))
	GC(t, target)

	// Expect that the target contains id1, id2, id3
	ExpectExists(t, target, id1)
	ExpectExists(t, target, id2)
	ExpectExists(t, target, id3)

	// Next transaction, remove /a/b
	tr = memstore.NewTransaction()
	Remove(t, st, tr, "/a/b")
	Commit(t, tr)

	cb, err = stream.Recv()
	if err != nil {
		t.Fatalf("Recv() failed: %v", err)
	}
	// Update target
	PutMutations(t, target, Mutations(cb.Changes))
	GC(t, target)

	// Expect that the target contains id1, id2, but not id3
	ExpectExists(t, target, id1)
	ExpectExists(t, target, id2)
	ExpectNotExists(t, target, id3)
}
