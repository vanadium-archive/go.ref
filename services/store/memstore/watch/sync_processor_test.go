package watch

import (
	"testing"

	"veyron/services/store/memstore"
	"veyron2/storage"
)

func TestSyncProcessState(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Put /, /a, /a/b
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	// Remove /a/b
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a/b")
	commit(t, tr)
	gc(t, st)

	if err := st.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Re-create a new store. This should compress the log, creating an initial
	// state containing / and /a.
	st, cleanup = openStore(t, dbName)
	defer cleanup()

	log, cleanup, processor := openLog(t, dbName)
	defer cleanup()

	post1 := st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Expect initial state that
	// 1) Contains / with value val1 and implicit directory entry /a
	// 2) Contains /a with value val2
	logstore, err := log.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("ReadState() failed: %v", err)
	}
	logst := logstore.State
	changes, err := processor.processState(logst)
	if len(changes) != 2 {
		t.Fatalf("Expected changes to have 2 entries, got: %v", changes)
	}
	expectExists(t, changes, id1, storage.NoVersion, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", empty)
}

func TestSyncProcessTransactionAddRemove(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup, processor := openLog(t, dbName)
	defer cleanup()

	logstore, err := log.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("ReadState() failed: %v", err)
	}
	logst := logstore.State
	changes, err := processor.processState(logst)
	if err != nil {
		t.Fatalf("processState() failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("Expected changes to have 0 entries")
	}

	// First transaction, put /, /a, /a/b
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	id3 := put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	post1 := st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version
	post3 := st.Snapshot().Find(id3).Version

	// Expect transaction that
	// 1) Adds / with value val1 and implicit directory entry /a
	// 2) Adds /a with value val2 and implicit directory entry /a/b
	// 3) Adds /a/b with value val3
	logmu, err := log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 3 {
		t.Fatal("Expected changes to have 3 entries")
	}
	expectExists(t, changes, id1, storage.NoVersion, post1, true, "val1", dir("a", id2))
	expectExists(t, changes, id2, storage.NoVersion, post2, false, "val2", dir("b", id3))
	expectExists(t, changes, id3, storage.NoVersion, post3, false, "val3", empty)

	// Next transaction, remove /a/b
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a/b")
	commit(t, tr)

	pre2 := post2
	pre3 := post3
	post2 = st.Snapshot().Find(id2).Version

	// Expect transaction that removes implicit dir entry /a/b from /a
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatal("Expected changes to have 1 entry")
	}
	expectExists(t, changes, id2, pre2, post2, false, "val2", empty)

	// Garbage-collect the node at /a/b
	gc(t, st)

	// Expect transaction that deletes the node at /a/b
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatal("Expected changes to have 1 entry")
	}
	expectDoesNotExist(t, changes, id3, pre3, false)
}

func TestSyncProcessTransactionRemoveRecursive(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup, processor := openLog(t, dbName)
	defer cleanup()

	processor, err := newSyncProcessor(rootPublicID)
	if err != nil {
		t.Fatalf("newSyncProcessor() failed: %v", err)
	}

	logstore, err := log.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("ReadState() failed: %v", err)
	}
	logst := logstore.State
	changes, err := processor.processState(logst)
	if err != nil {
		t.Fatalf("processState() failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("Expected changes to have 0 entries")
	}

	// First transaction, put /, /a, /a/b
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	id3 := put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	post1 := st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version
	post3 := st.Snapshot().Find(id3).Version

	// Assume the first transaction
	// 1) Adds / with value val1 and implicit directory entry /a
	// 2) Adds /a with value val2 and implicit directory entry /a/b
	// 3) Adds /a/b with value val3
	logmu, err := log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	if _, err := processor.processTransaction(logmu); err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}

	// Next transaction, remove /a
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a")
	commit(t, tr)

	pre1 := post1
	pre2 := post2
	pre3 := post3
	post1 = st.Snapshot().Find(id1).Version

	// Expect transaction that removes implicit dir entry /a from /
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatal("Expected changes to have 1 entry")
	}
	expectExists(t, changes, id1, pre1, post1, true, "val1", empty)

	// Garbage-collect the nodes at /a and /a/b
	gc(t, st)

	// Expect transaction that deletes the nodes at /a and /a/b
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatal("Expected changes to have 2 entries")
	}
	expectDoesNotExist(t, changes, id2, pre2, false)
	expectDoesNotExist(t, changes, id3, pre3, false)
}

func TestSyncProcessTransactionUpdateRemoveRoot(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup, processor := openLog(t, dbName)
	defer cleanup()

	processor, err := newSyncProcessor(rootPublicID)
	if err != nil {
		t.Fatalf("newSyncProcessor() failed: %v", err)
	}

	logstore, err := log.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("ReadState() failed: %v", err)
	}
	logst := logstore.State
	changes, err := processor.processState(logst)
	if err != nil {
		t.Fatalf("processState() failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("Expected changes to have 0 entries")
	}

	// First transaction, put /, /a
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	post1 := st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Assume the first transaction
	// 1) Adds / with value val1 and implicit directory entry /a
	// 2) Adds /a with value val2
	logmu, err := log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	if _, err := processor.processTransaction(logmu); err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}

	// Next transaction, update /
	tr = memstore.NewTransaction()
	put(t, st, tr, "/", "val3")
	commit(t, tr)

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version

	// Expect transaction that updates / with value val3
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatal("Expected changes to have 1 entry")
	}
	expectExists(t, changes, id1, pre1, post1, true, "val3", dir("a", id2))

	// Next transaction, remove /
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/")
	commit(t, tr)

	pre1 = post1
	pre2 := post2

	// Expect a transaction that deletes /
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatal("Expected changes to have 1 entry")
	}
	expectDoesNotExist(t, changes, id1, pre1, true)

	// Garbage-collect the nodes at / and /a
	gc(t, st)

	// Expect transaction that deletes the nodes at / and /a
	logmu, err = log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	changes, err = processor.processTransaction(logmu)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatal("Expected changes to have 1 entry")
	}
	expectDoesNotExist(t, changes, id2, pre2, false)
}
