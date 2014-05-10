package state

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"veyron/services/store/memstore/refs"

	"veyron/runtimes/google/lib/functional"
	"veyron2/vom"
)

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

// expectEqCells compares two *cell values.
func expectEqCells(t *testing.T, file string, line int, c1, c2 *Cell) {
	expectEqValues(t, file, line, c1.Value, c2.Value)
	expectEqImplicitDir(t, file, line, c1.Dir, c2.Dir)
}

// expectEqIDTables compares two states.
func expectEqIDTables(t *testing.T, t1, t2 functional.Set) {
	_, file, line, _ := runtime.Caller(1)
	t1.Iter(func(it1 interface{}) bool {
		c1 := it1.(*Cell)
		it2, ok := t2.Get(c1)
		if !ok {
			t.Errorf("%s(%d): cell does not exist: %v", file, line, c1)
		} else {
			c2 := it2.(*Cell)
			expectEqCells(t, file, line, c1, c2)
		}
		return true
	})
	t2.Iter(func(it2 interface{}) bool {
		_, ok := t1.Get(it2)
		if !ok {
			t.Errorf("%s(%d): cell should not exist: %v", file, line, it2)
		}
		return true
	})
}

func TestState(t *testing.T) {
	dbName, err := ioutil.TempDir(os.TempDir(), "store")
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	defer os.Remove(dbName)

	dbFile := filepath.Join(dbName, "db")
	ofile, err := os.Create(dbFile)
	if err != nil {
		t.Fatalf("Error opening log file: %s", err)
	}
	defer ofile.Close()
	enc := vom.NewEncoder(ofile)

	// Create an initial state.
	st1 := New(rootPublicID)
	sn := st1.MutableSnapshot()
	mkdir(t, sn, "/")
	mkdir(t, sn, "/a")
	mkdir(t, sn, "/a/b")
	mkdir(t, sn, "/a/b/c")
	mkdir(t, sn, "/a/b/c/d")
	if err := st1.ApplyMutations(sn.Mutations()); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if err := st1.Write(enc); err != nil {
		t.Errorf("Error writing log: %s", err)
	}

	ifile, err := os.Open(dbFile)
	if err != nil {
		t.Fatalf("Error opening log file: %s")
	}
	defer ifile.Close()
	dec := vom.NewDecoder(ifile)
	st2 := New(rootPublicID)
	if err := st2.Read(dec); err != nil {
		t.Fatalf("Can't read state: %s", err)
	}

	expectEqIDTables(t, st1.snapshot.idTable, st2.snapshot.idTable)
}
