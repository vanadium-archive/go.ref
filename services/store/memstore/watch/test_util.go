package watch

import (
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"veyron/services/store/memstore"
	"veyron/services/store/raw"
	"veyron/services/store/service"

	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
)

func get(t *testing.T, st *memstore.Store, tr service.Transaction, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, st *memstore.Store, tr service.Transaction, path string, v interface{}) storage.ID {
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

func remove(t *testing.T, st *memstore.Store, tr service.Transaction, path string) {
	if err := st.Bind(path).Remove(rootPublicID, tr); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func commit(t *testing.T, tr service.Transaction) {
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

func openLog(t *testing.T, dbName string) (*memstore.RLog, func(), reqProcessor) {
	log, err := memstore.OpenLog(dbName, true)
	if err != nil {
		t.Fatalf("openLog() failed: %v", err)
	}
	cleanup := func() {
		log.Close()
	}

	processor, err := newRawProcessor(rootPublicID)
	if err != nil {
		cleanup()
		t.Fatalf("newRawProcessor() failed: %v", err)
	}

	return log, cleanup, processor
}

func createWatcher(t *testing.T, dbName string) (service.Watcher, func()) {
	w, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return w, func() {
		w.Close()
	}
}

var (
	empty = []storage.DEntry{}
)

func dir(name string, id storage.ID) []storage.DEntry {
	return []storage.DEntry{storage.DEntry{
		Name: name,
		ID:   id,
	}}
}

func expectInitialStateSkipped(t *testing.T, change watch.Change) {
	if change.Name != "" {
		t.Fatalf("Expect Name to be \"\" but was: %v", change.Name)
	}
	if change.State != watch.InitialStateSkipped {
		t.Fatalf("Expect State to be InitialStateSkipped but was: %v", change.State)
	}
	if len(change.ResumeMarker) != 0 {
		t.Fatalf("Expect no ResumeMarker but was: %v", change.ResumeMarker)
	}
}

func expectExists(t *testing.T, changes []watch.Change, id storage.ID, pre, post storage.Version, isRoot bool, value string, dir []storage.DEntry) {
	change := findChange(t, changes, id)
	if change.State != watch.Exists {
		t.Fatalf("Expected id to exist: %v", id)
	}
	cv := change.Value.(*raw.Mutation)
	if cv.PriorVersion != pre {
		t.Fatalf("Expected PriorVersion to be %v, but was: %v", pre, cv.PriorVersion)
	}
	if cv.Version != post {
		t.Fatalf("Expected Version to be %v, but was: %v", post, cv.Version)
	}
	if cv.IsRoot != isRoot {
		t.Fatalf("Expected IsRoot to be: %v, but was: %v", isRoot, cv.IsRoot)
	}
	if cv.Value != value {
		t.Fatalf("Expected Value to be: %v, but was: %v", value, cv.Value)
	}
	expectDirEquals(t, cv.Dir, dir)
}

func expectDoesNotExist(t *testing.T, changes []watch.Change, id storage.ID, pre storage.Version, isRoot bool) {
	change := findChange(t, changes, id)
	if change.State != watch.DoesNotExist {
		t.Fatalf("Expected id to not exist: %v", id)
	}
	cv := change.Value.(*raw.Mutation)
	if cv.PriorVersion != pre {
		t.Fatalf("Expected PriorVersion to be %v, but was: %v", pre, cv.PriorVersion)
	}
	if cv.Version != storage.NoVersion {
		t.Fatalf("Expected Version to be NoVersion, but was: %v", cv.Version)
	}
	if cv.IsRoot != isRoot {
		t.Fatalf("Expected IsRoot to be: %v, but was: %v", isRoot, cv.IsRoot)
	}
	if cv.Value != nil {
		t.Fatal("Expected Value to be nil")
	}
	if cv.Dir != nil {
		t.Fatal("Expected Dir to be nil")
	}
}

func findChange(t *testing.T, changes []watch.Change, id storage.ID) watch.Change {
	for _, change := range changes {
		cv, ok := change.Value.(*raw.Mutation)
		if !ok {
			t.Fatal("Expected a Mutation")
		}
		if cv.ID == id {
			return change
		}
	}
	t.Fatalf("Expected a change for id: %v", id)
	panic("should not reach here")
}

func expectDirEquals(t *testing.T, actual, expected []storage.DEntry) {
	if len(actual) != len(expected) {
		t.Fatalf("Expected Dir to have %v refs, but had %v", len(expected), len(actual))
	}
	for i, e := range expected {
		a := actual[i]
		if a != e {
			t.Fatalf("Expected Dir entry %v to be %v, but was %v", i, e, a)
		}
	}
}
