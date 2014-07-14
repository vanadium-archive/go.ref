package watch

import (
	"testing"

	"veyron/services/store/memstore"
	watchtesting "veyron/services/store/memstore/testing"

	"veyron2/storage"
)

func TestRawProcessState(t *testing.T) {
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

	log, cleanup := openLog(t, dbName)
	defer cleanup()
	processor := createRawProcessor(t)

	post1 := st.Snapshot().Find(id1).Version
	post2 := st.Snapshot().Find(id2).Version

	// Expect initial state that
	// 1) Contains / with value val1 and implicit directory entry /a
	// 2) Contains /a with value val2
	changes := expectState(t, log, processor, 2)
	watchtesting.ExpectMutationExists(t, changes, id1, storage.NoVersion, post1, true, "val1", watchtesting.DirOf("a", id2))
	watchtesting.ExpectMutationExists(t, changes, id2, storage.NoVersion, post2, false, "val2", watchtesting.EmptyDir)
}

func TestRawProcessTransactionAddRemove(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()
	processor := createRawProcessor(t)

	expectState(t, log, processor, 0)

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
	changes := expectTransaction(t, log, processor, 3)
	watchtesting.ExpectMutationExists(t, changes, id1, storage.NoVersion, post1, true, "val1", watchtesting.DirOf("a", id2))
	watchtesting.ExpectMutationExists(t, changes, id2, storage.NoVersion, post2, false, "val2", watchtesting.DirOf("b", id3))
	watchtesting.ExpectMutationExists(t, changes, id3, storage.NoVersion, post3, false, "val3", watchtesting.EmptyDir)

	// Next transaction, remove /a/b
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a/b")
	commit(t, tr)

	pre2 := post2
	pre3 := post3
	post2 = st.Snapshot().Find(id2).Version

	// Expect transaction that removes implicit dir entry /a/b from /a
	changes = expectTransaction(t, log, processor, 1)
	watchtesting.ExpectMutationExists(t, changes, id2, pre2, post2, false, "val2", watchtesting.EmptyDir)

	// Garbage-collect the node at /a/b
	gc(t, st)

	// Expect transaction that deletes the node at /a/b
	changes = expectTransaction(t, log, processor, 1)
	watchtesting.ExpectMutationDoesNotExist(t, changes, id3, pre3, false)
}

func TestRawProcessTransactionRemoveRecursive(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()
	processor := createRawProcessor(t)

	processor, err := newRawProcessor(rootPublicID)
	if err != nil {
		t.Fatalf("newRawProcessor() failed: %v", err)
	}

	expectState(t, log, processor, 0)

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
	expectTransaction(t, log, processor, 3)

	// Next transaction, remove /a
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a")
	commit(t, tr)

	pre1 := post1
	pre2 := post2
	pre3 := post3
	post1 = st.Snapshot().Find(id1).Version

	// Expect transaction that removes implicit dir entry /a from /
	changes := expectTransaction(t, log, processor, 1)
	watchtesting.ExpectMutationExists(t, changes, id1, pre1, post1, true, "val1", watchtesting.EmptyDir)

	// Garbage-collect the nodes at /a and /a/b
	gc(t, st)

	// Expect transaction that deletes the nodes at /a and /a/b
	changes = expectTransaction(t, log, processor, 2)
	watchtesting.ExpectMutationDoesNotExist(t, changes, id2, pre2, false)
	watchtesting.ExpectMutationDoesNotExist(t, changes, id3, pre3, false)
}

func TestRawProcessTransactionUpdateRemoveRoot(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()
	processor := createRawProcessor(t)

	processor, err := newRawProcessor(rootPublicID)
	if err != nil {
		t.Fatalf("newRawProcessor() failed: %v", err)
	}

	expectState(t, log, processor, 0)

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
	expectTransaction(t, log, processor, 2)

	// Next transaction, update /
	tr = memstore.NewTransaction()
	put(t, st, tr, "/", "val3")
	commit(t, tr)

	pre1 := post1
	post1 = st.Snapshot().Find(id1).Version

	// Expect transaction that updates / with value val3
	changes := expectTransaction(t, log, processor, 1)
	watchtesting.ExpectMutationExists(t, changes, id1, pre1, post1, true, "val3", watchtesting.DirOf("a", id2))

	// Next transaction, remove /
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/")
	commit(t, tr)

	pre1 = post1
	pre2 := post2

	// Expect a transaction that deletes /
	changes = expectTransaction(t, log, processor, 1)
	watchtesting.ExpectMutationDoesNotExist(t, changes, id1, pre1, true)

	// Garbage-collect the nodes at / and /a
	gc(t, st)

	// Expect transaction that deletes the nodes at / and /a
	changes = expectTransaction(t, log, processor, 1)
	watchtesting.ExpectMutationDoesNotExist(t, changes, id2, pre2, false)
}
