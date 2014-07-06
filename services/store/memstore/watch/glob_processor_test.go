package watch

import (
	"testing"

	"veyron/services/store/memstore"
	watchtesting "veyron/services/store/memstore/watch/testing"

	"veyron2/storage"
)

func TestGlobProcessState(t *testing.T) {
	// Create a new store.
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	// Put /, /a, /a/b.
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	put(t, st, tr, "/a/b", "val3")
	id4 := put(t, st, tr, "/a/c", "val4")
	// Test duplicate paths to the same object.
	put(t, st, tr, "/a/d", id4)
	commit(t, tr)

	// Remove /a/b.
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

	rootRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/"), "...")
	rootListProcessor := createGlobProcessor(t, storage.ParsePath("/"), "*")
	aRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "...")
	aListProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "*")

	// Expect initial state that contains /, /a, /a/c and /a/d.
	logst := readState(t, log)

	changes := processState(t, rootRecursiveProcessor, logst, 4)
	watchtesting.ExpectEntryExists(t, changes, "", id1, "val1")
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")
	watchtesting.ExpectEntryExists(t, changes, "a/c", id4, "val4")
	watchtesting.ExpectEntryExists(t, changes, "a/d", id4, "val4")

	changes = processState(t, rootListProcessor, logst, 1)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")

	changes = processState(t, aRecursiveProcessor, logst, 3)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")
	watchtesting.ExpectEntryExists(t, changes, "a/c", id4, "val4")
	watchtesting.ExpectEntryExists(t, changes, "a/d", id4, "val4")

	processState(t, aListProcessor, logst, 2)
	watchtesting.ExpectEntryExists(t, changes, "a/c", id4, "val4")
	watchtesting.ExpectEntryExists(t, changes, "a/d", id4, "val4")
}

func TestGlobProcessTransactionAdd(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()

	rootRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/"), "...")
	rootListProcessor := createGlobProcessor(t, storage.ParsePath("/"), "*")
	aRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "...")
	aListProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "*")
	bRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a/b"), "...")

	logst := readState(t, log)
	processState(t, rootRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, rootListProcessor, logst.DeepCopy(), 0)
	processState(t, aRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, aListProcessor, logst.DeepCopy(), 0)
	processState(t, bRecursiveProcessor, logst.DeepCopy(), 0)

	// First transaction, put /, /a, /a/b.
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	id3 := put(t, st, tr, "/a/b", "val3")
	id4 := put(t, st, tr, "/a/c", "val4")
	// Test duplicate paths to the same object.
	put(t, st, tr, "/a/d", id4)
	commit(t, tr)

	// Expect transaction that adds /, /a and /a/b.
	mus := readTransaction(t, log)

	changes := processTransaction(t, rootRecursiveProcessor, mus, 5)
	watchtesting.ExpectEntryExists(t, changes, "", id1, "val1")
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val3")
	watchtesting.ExpectEntryExists(t, changes, "a/c", id4, "val4")
	watchtesting.ExpectEntryExists(t, changes, "a/d", id4, "val4")

	changes = processTransaction(t, rootListProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")

	changes = processTransaction(t, aRecursiveProcessor, mus, 4)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val3")
	watchtesting.ExpectEntryExists(t, changes, "a/c", id4, "val4")
	watchtesting.ExpectEntryExists(t, changes, "a/d", id4, "val4")

	changes = processTransaction(t, aListProcessor, mus, 3)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val3")
	watchtesting.ExpectEntryExists(t, changes, "a/c", id4, "val4")
	watchtesting.ExpectEntryExists(t, changes, "a/d", id4, "val4")

	changes = processTransaction(t, bRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val3")
}

func TestGlobProcessTransactionEmptyPath(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()

	bRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a/b"), "...")

	expectState(t, log, bRecursiveProcessor, 0)

	// First transaction, put /, /a.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	put(t, st, tr, "/a", "val2")
	commit(t, tr)

	// Expect no change.
	expectTransaction(t, log, bRecursiveProcessor, 0)

	// Next transaction, put /a/b.
	tr = memstore.NewTransaction()
	id3 := put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	// Expect transaction that adds /a/b.
	changes := expectTransaction(t, log, bRecursiveProcessor, 1)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val3")
}

func TestGlobProcessTransactionUpdate(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()

	rootRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/"), "...")
	rootListProcessor := createGlobProcessor(t, storage.ParsePath("/"), "*")
	aRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "...")
	aListProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "*")
	bRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a/b"), "...")

	logst := readState(t, log)
	processState(t, rootRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, rootListProcessor, logst.DeepCopy(), 0)
	processState(t, aRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, aListProcessor, logst.DeepCopy(), 0)
	processState(t, bRecursiveProcessor, logst.DeepCopy(), 0)

	// First transaction, put /, /a, /a/b.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	put(t, st, tr, "/a", "val2")
	id3 := put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	mus := readTransaction(t, log)
	changes := processTransaction(t, rootRecursiveProcessor, mus, 3)
	changes = processTransaction(t, rootListProcessor, mus, 1)
	changes = processTransaction(t, aRecursiveProcessor, mus, 2)
	changes = processTransaction(t, aListProcessor, mus, 1)
	changes = processTransaction(t, bRecursiveProcessor, mus, 1)

	// Next transaction, remove /a/b.
	tr = memstore.NewTransaction()
	put(t, st, tr, "/a/b", "val4")
	commit(t, tr)

	// Expect transaction that updates /a/b.
	mus = readTransaction(t, log)

	changes = processTransaction(t, rootRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val4")

	processTransaction(t, rootListProcessor, mus, 0)

	changes = processTransaction(t, aRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val4")

	processTransaction(t, aListProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val4")

	processTransaction(t, bRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a/b", id3, "val4")
}

func TestGlobProcessTransactionRemove(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()

	rootRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/"), "...")
	rootListProcessor := createGlobProcessor(t, storage.ParsePath("/"), "*")
	aRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "...")
	aListProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "*")
	bRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a/b"), "...")

	logst := readState(t, log)
	processState(t, rootRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, rootListProcessor, logst.DeepCopy(), 0)
	processState(t, aRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, aListProcessor, logst.DeepCopy(), 0)
	processState(t, bRecursiveProcessor, logst.DeepCopy(), 0)

	// First transaction, put /, /a, /a/b.
	tr := memstore.NewTransaction()
	put(t, st, tr, "/", "val1")
	id2 := put(t, st, tr, "/a", "val2")
	put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	mus := readTransaction(t, log)
	processTransaction(t, rootRecursiveProcessor, mus, 3)
	processTransaction(t, rootListProcessor, mus, 1)
	processTransaction(t, aRecursiveProcessor, mus, 2)
	processTransaction(t, aListProcessor, mus, 1)
	processTransaction(t, bRecursiveProcessor, mus, 1)

	// Next transaction, remove /a/b.
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a/b")
	commit(t, tr)

	// Expect transaction that updates /a and removes /a/b.
	mus = readTransaction(t, log)

	changes := processTransaction(t, rootRecursiveProcessor, mus, 2)
	// TODO(tilaks): Should we report implicit directory changes?
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	changes = processTransaction(t, rootListProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")

	changes = processTransaction(t, aRecursiveProcessor, mus, 2)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	changes = processTransaction(t, aListProcessor, mus, 1)
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	changes = processTransaction(t, bRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	// Garbage-collect the node at /a/b.
	gc(t, st)

	// Expect no change.
	mus = readTransaction(t, log)
	processTransaction(t, rootRecursiveProcessor, mus, 0)
	processTransaction(t, rootListProcessor, mus, 0)
	processTransaction(t, aRecursiveProcessor, mus, 0)
	processTransaction(t, aListProcessor, mus, 0)
	processTransaction(t, bRecursiveProcessor, mus, 0)
}

func TestGlobProcessTransactionRemoveRecursive(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()

	rootRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/"), "...")
	rootListProcessor := createGlobProcessor(t, storage.ParsePath("/"), "*")
	aRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "...")
	aListProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "*")
	bRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a/b"), "...")

	logst := readState(t, log)
	processState(t, rootRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, rootListProcessor, logst.DeepCopy(), 0)
	processState(t, aRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, aListProcessor, logst.DeepCopy(), 0)
	processState(t, bRecursiveProcessor, logst.DeepCopy(), 0)

	// First transaction, put /, /a, /a/b.
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	put(t, st, tr, "/a", "val2")
	put(t, st, tr, "/a/b", "val3")
	commit(t, tr)

	mus := readTransaction(t, log)
	processTransaction(t, rootRecursiveProcessor, mus, 3)
	processTransaction(t, rootListProcessor, mus, 1)
	processTransaction(t, aRecursiveProcessor, mus, 2)
	processTransaction(t, aListProcessor, mus, 1)
	processTransaction(t, bRecursiveProcessor, mus, 1)

	// Next transaction, remove /a.
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a")
	commit(t, tr)

	// Expect transaction that removes /a and /a/b.
	mus = readTransaction(t, log)

	changes := processTransaction(t, rootRecursiveProcessor, mus, 3)
	// TODO(tilaks): Should we report implicit directory changes?
	watchtesting.ExpectEntryExists(t, changes, "", id1, "val1")
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a")
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	changes = processTransaction(t, rootListProcessor, mus, 1)
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a")

	changes = processTransaction(t, aRecursiveProcessor, mus, 2)
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a")
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	changes = processTransaction(t, aListProcessor, mus, 1)
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")

	changes = processTransaction(t, bRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryDoesNotExist(t, changes, "a/b")
}

func TestGlobProcessTransactionReplace(t *testing.T) {
	dbName, st, cleanup := createStore(t)
	defer cleanup()

	log, cleanup := openLog(t, dbName)
	defer cleanup()

	rootRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/"), "...")
	rootListProcessor := createGlobProcessor(t, storage.ParsePath("/"), "*")
	aRecursiveProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "...")
	aListProcessor := createGlobProcessor(t, storage.ParsePath("/a"), "*")

	logst := readState(t, log)

	processState(t, rootRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, rootListProcessor, logst.DeepCopy(), 0)
	processState(t, aRecursiveProcessor, logst.DeepCopy(), 0)
	processState(t, aListProcessor, logst.DeepCopy(), 0)

	// First transaction, put /, /a, /a/b.
	tr := memstore.NewTransaction()
	id1 := put(t, st, tr, "/", "val1")
	put(t, st, tr, "/a", "val2")
	commit(t, tr)

	mus := readTransaction(t, log)
	processTransaction(t, rootRecursiveProcessor, mus, 2)
	processTransaction(t, rootListProcessor, mus, 1)
	processTransaction(t, aRecursiveProcessor, mus, 1)
	processTransaction(t, aListProcessor, mus, 0)

	// Next transaction, replace /a.
	tr = memstore.NewTransaction()
	remove(t, st, tr, "/a")
	id2 := put(t, st, tr, "/a", "val2")
	commit(t, tr)

	// Expect transaction that updates /a.
	mus = readTransaction(t, log)

	changes := processTransaction(t, rootRecursiveProcessor, mus, 2)
	// TODO(tilaks): Should we report implicit directory changes?
	watchtesting.ExpectEntryExists(t, changes, "", id1, "val1")
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")

	changes = processTransaction(t, rootListProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")

	changes = processTransaction(t, aRecursiveProcessor, mus, 1)
	watchtesting.ExpectEntryExists(t, changes, "a", id2, "val2")

	processTransaction(t, aListProcessor, mus, 0)
}

// TODO(tilaks): test ACL update.
