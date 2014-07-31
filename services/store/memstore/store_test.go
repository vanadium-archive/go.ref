package memstore

import (
	"io/ioutil"
	"os"
	"testing"

	storetesting "veyron/services/store/memstore/testing"
	"veyron/services/store/raw"

	"veyron2/storage"
	"veyron2/verror"
)

func TestLogWrite(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create the state. This should also initialize the log.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("newState() failed: %v", err)
	}

	// Open the log for reading.
	log, err := OpenLog(dbName, true)
	if err != nil {
		t.Fatalf("OpenLog() failed: %v", err)
	}

	// The log should be sync'ed, test reading the initial state.
	logst, err := log.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("ReadState() failed: %v", err)
	}

	// Construct a transaction.
	v1 := "v1"
	v2 := "v2"
	v3 := "v3"
	tr := NewTransaction()
	put(t, st, tr, "/", v1)
	put(t, st, tr, "/a", v2)
	put(t, st, tr, "/a/b", v3)
	commit(t, tr)

	// Check that the mutations were applied to the state.
	expectValue(t, st, nil, "/", v1)
	expectValue(t, st, nil, "/a", v2)
	expectValue(t, st, nil, "/a/b", v3)

	// The log should be sync'ed, test reading the transaction.
	logmu, err := log.ReadTransaction()
	if err != nil {
		t.Fatalf("ReadTransaction() failed: %v", err)
	}
	logst.ApplyMutations(logmu)
	expectValue(t, logst, nil, "/", v1)
	expectValue(t, logst, nil, "/a", v2)
	expectValue(t, logst, nil, "/a/b", v3)
}

func TestFailedLogWrite(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}

	// Create the state. This should also initialize the log.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("newState() failed: %v", err)
	}

	// Construct a transaction.
	v1 := "v1"
	tr := NewTransaction()
	put(t, st, tr, "/", v1)
	v2 := "v2"
	put(t, st, tr, "/a", v2)

	// Close the log file. Subsequent writes to the log should fail.
	st.log.close()

	// Commit the state. The call should fail.
	if err := st.log.appendTransaction(nil); !verror.Is(err, verror.Aborted) {
		t.Errorf("Expected error %v, got %v", verror.Aborted, err)
	}
}

func TestRecoverFromLog(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create the state. This should also initialize the log.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("newState() failed: %v", err)
	}

	// Construct a transaction.
	v1 := "v1"
	v2 := "v2"
	v3 := "v3"
	tr := NewTransaction()
	put(t, st, tr, "/", v1)
	put(t, st, tr, "/a", v2)
	put(t, st, tr, "/a/b", v3)
	commit(t, tr)

	// Recover state from the log.
	recoverst, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("newState() failed: %v", err)
	}
	expectValue(t, recoverst, nil, "/", v1)
	expectValue(t, recoverst, nil, "/a", v2)
	expectValue(t, recoverst, nil, "/a/b", v3)
}

func TestPutMutations(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create the state.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Add /, /a, /a/b
	id1, id2, id3 := storage.NewID(), storage.NewID(), storage.NewID()
	pre1, pre2, pre3 := storage.NoVersion, storage.NoVersion, storage.NoVersion
	post1, post2, post3 := storage.NewVersion(), storage.NewVersion(), storage.NewVersion()
	v1, v2, v3 := "v1", "v2", "v3"

	storetesting.PutMutationsBatch(t, rootPublicID, st.PutMutations, []raw.Mutation{
		raw.Mutation{
			ID:           id1,
			PriorVersion: pre1,
			Version:      post1,
			IsRoot:       true,
			Value:        v1,
			Dir:          dir("a", id2),
		},
		raw.Mutation{
			ID:           id2,
			PriorVersion: pre2,
			Version:      post2,
			IsRoot:       false,
			Value:        v2,
			Dir:          dir("b", id3),
		},
		raw.Mutation{
			ID:           id3,
			PriorVersion: pre3,
			Version:      post3,
			IsRoot:       false,
			Value:        v3,
			Dir:          empty,
		},
	})

	expectValue(t, st, nil, "/", v1)
	expectValue(t, st, nil, "/a", v2)
	expectValue(t, st, nil, "/a/b", v3)

	// Remove /a/b
	pre1, pre2, pre3 = post1, post2, post3
	post2 = storage.NewVersion()

	storetesting.PutMutationsBatch(t, rootPublicID, st.PutMutations, []raw.Mutation{
		raw.Mutation{
			ID: id2, PriorVersion: pre2,
			Version: post2,
			IsRoot:  false,
			Value:   v2,
			Dir:     empty,
		}})

	expectValue(t, st, nil, "/", v1)
	expectValue(t, st, nil, "/a", v2)
	expectNotExists(t, st, nil, "a/b")

	// Garbage-collect /a/b
	post3 = storage.NoVersion

	storetesting.PutMutationsBatch(t, rootPublicID, st.PutMutations, []raw.Mutation{
		raw.Mutation{
			ID:           id3,
			PriorVersion: pre3,
			Version:      post3,
			IsRoot:       false,
		}})

	expectValue(t, st, nil, "/", v1)
	expectValue(t, st, nil, "/a", v2)
	expectNotExists(t, st, nil, "a/b")

	// Remove /
	pre1, pre2, pre3 = post1, post2, post3
	post1 = storage.NoVersion

	storetesting.PutMutationsBatch(t, rootPublicID, st.PutMutations, []raw.Mutation{
		raw.Mutation{
			ID:           id1,
			PriorVersion: pre1,
			Version:      post1,
			IsRoot:       true,
		}})

	expectNotExists(t, st, nil, "/")
	expectNotExists(t, st, nil, "/a")
	expectNotExists(t, st, nil, "a/b")

	// Garbage-collect /a
	post2 = storage.NoVersion

	storetesting.PutMutationsBatch(t, rootPublicID, st.PutMutations, []raw.Mutation{
		raw.Mutation{
			ID:           id2,
			PriorVersion: pre2,
			Version:      post2,
			IsRoot:       false,
		}})

	expectNotExists(t, st, nil, "/")
	expectNotExists(t, st, nil, "/a")
	expectNotExists(t, st, nil, "a/b")
}

func TestPutConflictingMutations(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create the state.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Add /, /a
	id1, id2 := storage.NewID(), storage.NewID()
	pre1, pre2 := storage.NoVersion, storage.NoVersion
	post1, post2 := storage.NewVersion(), storage.NewVersion()
	v1, v2 := "v1", "v2"

	storetesting.PutMutationsBatch(t, rootPublicID, st.PutMutations, []raw.Mutation{
		raw.Mutation{
			ID:           id1,
			PriorVersion: pre1,
			Version:      post1,
			IsRoot:       true,
			Value:        v1,
			Dir:          dir("a", id2),
		},
		raw.Mutation{
			ID:           id2,
			PriorVersion: pre2,
			Version:      post2,
			IsRoot:       false,
			Value:        v2,
			Dir:          empty,
		},
	})

	expectValue(t, st, nil, "/", v1)
	expectValue(t, st, nil, "/a", v2)

	// Attempt to update /a with a bad precondition
	pre2 = storage.NewVersion()
	post2 = storage.NewVersion()
	v2 = "v4"

	s := storetesting.PutMutations(rootPublicID, st.PutMutations)
	s.SendStream().Send(raw.Mutation{
		ID:           id2,
		PriorVersion: pre2,
		Version:      post2,
		IsRoot:       true,
		Value:        v2,
		Dir:          empty,
	})
	if err := s.Finish(); !verror.Is(err, verror.Aborted) {
		t.Errorf("Error should be %v: got %v", verror.Aborted, err)
	}

}

func TestPutDuplicateMutations(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create the state.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	id := storage.NewID()
	s := storetesting.PutMutations(rootPublicID, st.PutMutations)
	s.SendStream().Send(raw.Mutation{
		ID:           id,
		PriorVersion: storage.NoVersion,
		Version:      storage.NewVersion(),
		IsRoot:       true,
		Value:        "v1",
		Dir:          empty,
	})
	s.SendStream().Send(raw.Mutation{
		ID:           id,
		PriorVersion: storage.NoVersion,
		Version:      storage.NewVersion(),
		IsRoot:       true,
		Value:        "v2",
		Dir:          empty,
	})
	if err := s.Finish(); !verror.Is(err, verror.BadArg) {
		t.Errorf("Error should be %v: got %v", verror.BadArg, err)
	}
}

func TestCancelPutMutation(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create the state.
	st, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	s := storetesting.PutMutations(rootPublicID, st.PutMutations)
	s.SendStream().Send(raw.Mutation{
		ID:           storage.NewID(),
		PriorVersion: storage.NoVersion,
		Version:      storage.NewVersion(),
		IsRoot:       true,
		Value:        "v1",
		Dir:          empty,
	})
	s.Cancel()
	if err := s.Finish(); !verror.Is(err, verror.Aborted) {
		t.Errorf("Error should be %v: got %v", verror.Aborted, err)
	}

	expectNotExists(t, st, nil, "/")
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
