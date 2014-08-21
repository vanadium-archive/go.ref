package watch

import (
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"veyron/services/store/memstore"
	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/services/watch/types"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
)

func get(t *testing.T, st *memstore.Store, tr *memstore.Transaction, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, st *memstore.Store, tr *memstore.Transaction, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	stat, err := st.Bind(path).Put(rootPublicID, tr, v)
	if err != nil {
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	if _, err := st.Bind(path).Get(rootPublicID, tr); err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	if stat != nil {
		return stat.ID
	}
	return storage.ID{}
}

func remove(t *testing.T, st *memstore.Store, tr *memstore.Transaction, path string) {
	if err := st.Bind(path).Remove(rootPublicID, tr); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func commit(t *testing.T, tr *memstore.Transaction) {
	if err := tr.Commit(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): Transaction aborted: %s", file, line, err)
	}
}

func gc(t *testing.T, st *memstore.Store) {
	if err := st.GC(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't gc: %s", file, line, err)
	}
}

func createStore(t *testing.T) (string, *memstore.Store, func()) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	cleanup := func() {
		os.RemoveAll(dbName)
	}

	st, err := memstore.New(rootPublicID, dbName)
	if err != nil {
		cleanup()
		t.Fatalf("memstore.New() failed: %v", err)
	}

	return dbName, st, cleanup
}

func openStore(t *testing.T, dbName string) (*memstore.Store, func()) {
	st, err := memstore.New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("memstore.New() failed: %v", err)
	}

	return st, func() {
		os.RemoveAll(dbName)
	}
}

func openLog(t *testing.T, dbName string) (*memstore.RLog, func()) {
	log, err := memstore.OpenLog(dbName, true)
	if err != nil {
		t.Fatalf("openLog() failed: %v", err)
	}

	return log, func() {
		log.Close()
	}
}

func createRawProcessor(t *testing.T) reqProcessor {
	processor, err := newRawProcessor(rootPublicID)
	if err != nil {
		t.Fatalf("newRawProcessor() failed: %v", err)
	}
	return processor
}

func createGlobProcessor(t *testing.T, path storage.PathName, pattern string) reqProcessor {
	processor, err := newGlobProcessor(rootPublicID, path, pattern)
	if err != nil {
		t.Fatalf("newGlobProcessor() failed: %v", err)
	}
	return processor
}

func createWatcher(t *testing.T, dbName string) (*Watcher, func()) {
	w, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return w, func() {
		w.Close()
	}
}

func expectState(t *testing.T, log *memstore.RLog, processor reqProcessor, numChanges int) []types.Change {
	st := readState(t, log)
	return processState(t, processor, st, numChanges)
}

func readState(t *testing.T, log *memstore.RLog) *state.State {
	st, err := log.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("ReadState() failed: %v", err)
	}
	return st.State
}

func processState(t *testing.T, processor reqProcessor, st *state.State, numChanges int) []types.Change {
	changes, err := processor.processState(st)
	if err != nil {
		t.Fatalf("processState() failed: %v", err)
	}
	if len(changes) != numChanges {
		t.Fatalf("Expected state to have %d changes, got %d", numChanges, len(changes))
	}
	return changes
}

func expectTransaction(t *testing.T, log *memstore.RLog, processor reqProcessor, numChanges int) []types.Change {
	mus := readTransaction(t, log)
	return processTransaction(t, processor, mus, numChanges)
}

func readTransaction(t *testing.T, log *memstore.RLog) *state.Mutations {
	mus, err := log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	return mus
}

func processTransaction(t *testing.T, processor reqProcessor, mus *state.Mutations, numChanges int) []types.Change {
	changes, err := processor.processTransaction(mus)
	if err != nil {
		t.Fatalf("processTransaction() failed: %v", err)
	}
	if len(changes) != numChanges {
		t.Fatalf("Expected transaction to have %d changes, got %d", numChanges, len(changes))
	}
	return changes
}
