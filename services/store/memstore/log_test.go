package memstore

import (
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"veyron/services/store/memstore/refs"
	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/storage"
	"veyron2/vom"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
)

type Dir struct {
	Entries map[string]storage.ID
}

type Data struct {
	Comment string
}

func init() {
	vom.Register(&Data{})
}

func newData(c string) *Data {
	return &Data{Comment: c}
}

func expectEqDir(t *testing.T, file string, line int, d1, d2 *Dir) {
	for name, id1 := range d1.Entries {
		id2, ok := d2.Entries[name]
		if !ok {
			t.Errorf("%s(%d): does not exist: %s", file, line, name)
		} else if id2 != id1 {
			t.Errorf("%s(%d): expected ID %s, got %s", file, line, id1, id2)
		}
	}
	for name, _ := range d2.Entries {
		_, ok := d1.Entries[name]
		if !ok {
			t.Errorf("%s(%d): should not exist: %s", file, line, name)
		}
	}
}

func expectEqData(t *testing.T, file string, line int, d1, d2 *Data) {
	if d1.Comment != d2.Comment {
		t.Errorf("%s(%d): expected %q, got %q", d1.Comment, d2.Comment)
	}
}

// expectEqValues compares two items.  They are equal only if they have the same
// type, and their contents are equal.
func expectEqValues(t *testing.T, file string, line int, v1, v2 interface{}) {
	switch x1 := v1.(type) {
	case *Dir:
		x2, ok := v2.(*Dir)
		if !ok {
			t.Errorf("%s(%d): not a Dir: %v", file, line, v2)
		} else {
			expectEqDir(t, file, line, x1, x2)
		}
	case *Data:
		x2, ok := v2.(*Data)
		if !ok {
			t.Errorf("%s(%d): not a Data: %v", file, line, v2)
		} else {
			expectEqData(t, file, line, x1, x2)
		}
	default:
		t.Errorf("Unknown type: %T, %v", v1, v1)
	}
}

// expectEqImplicitDir compares two directories.
func expectEqImplicitDir(t *testing.T, file string, line int, d1, d2 refs.Dir) {
	l1 := refs.FlattenDir(d1)
	l2 := refs.FlattenDir(d2)
	i1 := 0
	i2 := 0
	for i1 < len(l1) && i2 < len(l2) {
		e1 := l1[i1]
		e2 := l2[i2]
		if e1.Name == e2.Name {
			if e1.ID != e2.ID {
				t.Errorf("%s(%d): expected id %s, got %s", file, line, e1.ID, e2.ID)
			}
			i1++
			i2++
		} else if e1.Name < e2.Name {
			t.Errorf("%s(%d): missing directory %s", file, line, e1.Name)
			i1++
		} else {
			t.Errorf("%s(%d): unexpected directory %s", file, line, e2.Name)
			i2++
		}
	}
	for _, e1 := range l1[i1:] {
		t.Errorf("%s(%d): missing directory %s", file, line, e1.Name)
	}
	for _, e2 := range l2[i2:] {
		t.Errorf("%s(%d): unexpected directory %s", file, line, e2.Name)
	}
}

func readTransaction(r *RLog) (*state.Mutations, error) {
	type result struct {
		mu  *state.Mutations
		err error
	}
	results := make(chan result)
	go func() {
		defer close(results)
		mu, err := r.ReadTransaction()
		results <- result{mu: mu, err: err}
	}()
	res := <-results
	return res.mu, res.err
}

// readTransactions reads and applies the transactions.  Returns the last transaction read.
func readTransactions(t *testing.T, r *RLog, st *Store, n int) *state.Mutations {
	_, file, line, _ := runtime.Caller(1)
	var m *state.Mutations
	for i := 0; i < n; i++ {
		var err error
		m, err = readTransaction(r)
		if err != nil {
			t.Errorf("%s(%d): error in readTransaction(): %s", file, line, err)
			return nil
		}
		if err := st.ApplyMutations(m); err != nil {
			t.Errorf("%s(%d): error in ApplyMutations(): %s", file, line, err)
			return nil
		}
	}
	return m
}

func TestTransaction(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	w, err := createLog(dbName)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	// Create an initial state.
	st1, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("newState() failed: %v", err)
	}
	tr := &Transaction{}
	mkdir(t, st1, tr, "/")
	mkdir(t, st1, tr, "/a")
	mkdir(t, st1, tr, "/a/b")
	mkdir(t, st1, tr, "/a/b/c")
	commit(t, tr)

	// Write the initial state.
	defer w.close()
	w.writeState(st1)

	// Write some Transactions.
	var data [5]*Data
	var paths [5]string
	for i := 0; i != 5; i++ {
		name := fmt.Sprintf("data%d", i)
		tr := &Transaction{}
		data[i] = newData(name)
		paths[i] = "/a/b/c/" + name
		put(t, st1, tr, paths[i], data[i])
		commit(t, tr)
		w.appendTransaction(tr.snapshot.Mutations())
	}

	r, err := OpenLog(dbName, true)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	defer r.Close()
	st2, err := r.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("Can't read state: %s", err)
	}
	readTransactions(t, r, st2, 5)

	// Remove data3.
	{
		tr := &Transaction{}
		remove(t, st1, tr, "/a/b/c/data3")
		commit(t, tr)
		w.appendTransaction(tr.snapshot.Mutations())
	}
	readTransactions(t, r, st2, 1)
	{
		tr := &Transaction{}
		expectExists(t, st1, tr, paths[0])
		expectNotExists(t, st1, tr, paths[3])
	}

	// Remove all entries.
	{
		tr := &Transaction{}
		remove(t, st1, tr, "/a/b/c")
		commit(t, tr)
		w.appendTransaction(tr.snapshot.Mutations())
	}
	readTransactions(t, r, st2, 1)
	{
		tr := &Transaction{}
		for i := 0; i != 5; i++ {
			expectNotExists(t, st1, tr, paths[i])
		}
	}
}

func TestDeletions(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "vstore")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.RemoveAll(dbName)

	// Create an initial state.
	st1, err := New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("newState() failed: %v", err)
	}
	tr := &Transaction{}
	mkdir(t, st1, tr, "/")
	mkdir(t, st1, tr, "/a")
	ids := make(map[string]storage.ID)
	ids["/a/b"], _ = mkdir(t, st1, tr, "/a/b")
	ids["/a/b/c"], _ = mkdir(t, st1, tr, "/a/b/c")
	ids["/a/b/d"], _ = mkdir(t, st1, tr, "/a/b/d")
	commit(t, tr)

	// Reconstruct the state from the log.
	r, err := OpenLog(dbName, true)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	defer r.Close()
	st2, err := r.ReadState(rootPublicID)
	if err != nil {
		t.Fatalf("Can't read state: %s", err)
	}
	readTransactions(t, r, st2, 1)

	// Remove b.
	{
		tr := &Transaction{}
		remove(t, st1, tr, "/a/b")
		commit(t, tr)
	}
	readTransactions(t, r, st2, 1)
	{
		tr := &Transaction{}
		expectExists(t, st1, tr, "/a")
		expectNotExists(t, st1, tr, "/a/b")
	}

	// Perform a GC.
	if err := st1.GC(); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	// The transaction should include deletions.
	mu := readTransactions(t, r, st2, 1)
	for name, id := range ids {
		if _, ok := mu.Deletions[id]; !ok {
			t.Errorf("Expected deletion for path %s", name)
		}
		if len(mu.Deletions) != len(ids) {
			t.Errorf("Unexpected deletion: %v", mu.Deletions)
		}
	}
}
