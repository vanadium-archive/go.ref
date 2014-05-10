package memstore

import (
	"runtime"
	"testing"

	"veyron/services/store/service"
)

func newValue() interface{} {
	return &Dir{}
}

func TestPutGetRemoveRoot(t *testing.T) {
	st, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	o := st.Bind("/")
	testGetPutRemove(t, st, o)
}

func TestPutGetRemoveChild(t *testing.T) {
	st, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	tr := NewTransaction()
	mkdir(t, st, tr, "/")
	commit(t, tr)
	o := st.Bind("/Entries/a")
	testGetPutRemove(t, st, o)
}

func testGetPutRemove(t *testing.T, st *Store, o service.Object) {
	value := newValue()

	{
		// Check that the root object does not exist.
		tr := &Transaction{}
		if ok, err := o.Exists(rootPublicID, tr); ok && err == nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootPublicID, tr); v != nil && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}
	}

	{
		// Add the root object.
		tr1 := &Transaction{}
		s, err := o.Put(rootPublicID, tr1, value)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootPublicID, tr1); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		e, err := o.Get(rootPublicID, tr1)
		if err != nil {
			t.Errorf("Object should exist: %s", err)
		}
		if e.Stat.ID != s.ID {
			t.Errorf("Expected %s, got %s", s.ID, e.Stat.ID)
		}

		// Transactions are isolated.
		tr2 := &Transaction{}
		if ok, err := o.Exists(rootPublicID, tr2); ok && err != nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootPublicID, tr2); v != nil && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// Apply tr1.
		if err := tr1.Commit(); err != nil {
			t.Errorf("Unexpected error")
		}
		if ok, err := o.Exists(rootPublicID, tr1); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootPublicID, tr1); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// tr2 is still isolated.
		if ok, err := o.Exists(rootPublicID, tr2); ok && err == nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootPublicID, tr2); v != nil && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// tr3 observes the commit.
		tr3 := &Transaction{}
		if ok, err := o.Exists(rootPublicID, tr3); !ok || err != nil {
			t.Errorf("Should exist")
		}
		if _, err := o.Get(rootPublicID, tr3); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
	}

	{
		// Remove the root object.
		tr1 := &Transaction{}
		if err := o.Remove(rootPublicID, tr1); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootPublicID, tr1); ok && err != nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootPublicID, tr1); v != nil || err == nil {
			t.Errorf("Object should exist: %v", v)
		}

		// The removal is isolated.
		tr2 := &Transaction{}
		if ok, err := o.Exists(rootPublicID, tr2); !ok || err != nil {
			t.Errorf("Should exist")
		}
		if _, err := o.Get(rootPublicID, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// Apply tr1.
		if err := tr1.Commit(); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootPublicID, tr1); ok && err == nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootPublicID, tr1); v != nil || err == nil {
			t.Errorf("Object should exist: %v", v)
		}

		// The removal is isolated.
		if ok, err := o.Exists(rootPublicID, tr2); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootPublicID, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
	}

	{
		// Check that the root object does not exist.
		tr1 := &Transaction{}
		if ok, err := o.Exists(rootPublicID, tr1); ok && err == nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootPublicID, tr1); v != nil && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}
	}
}

func TestConcurrentOK(t *testing.T) {
	st, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	{
		tr := NewTransaction()
		mkdir(t, st, tr, "/")
		mkdir(t, st, tr, "/Entries/a")
		mkdir(t, st, tr, "/Entries/b")
		commit(t, tr)
	}

	o1 := st.Bind("/Entries/a/Entries/c")
	tr1 := &Transaction{}
	v1 := newValue()
	s1, err := o1.Put(rootPublicID, tr1, v1)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	o2 := st.Bind("/Entries/b/Entries/d")
	tr2 := &Transaction{}
	v2 := newValue()
	s2, err := o2.Put(rootPublicID, tr2, v2)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	if err := tr1.Commit(); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if err := tr2.Commit(); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	tr3 := &Transaction{}
	if x, err := o1.Get(rootPublicID, tr3); err != nil || x.Stat.ID != s1.ID {
		t.Errorf("Value should exist: %v, %s", x, err)
	}
	if x, err := o2.Get(rootPublicID, tr3); err != nil || x.Stat.ID != s2.ID {
		t.Errorf("Value should exist: %v, %s", x, err)
	}
}

func TestConcurrentConflict(t *testing.T) {
	st, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	{
		tr := NewTransaction()
		mkdir(t, st, tr, "/")
		mkdir(t, st, tr, "/Entries/a")
		commit(t, tr)
	}

	o := st.Bind("/Entries/a/Entries/c")
	tr1 := &Transaction{}
	v1 := newValue()
	s1, err := o.Put(rootPublicID, tr1, v1)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	tr2 := &Transaction{}
	v2 := newValue()
	if _, err = o.Put(rootPublicID, tr2, v2); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	if err := tr1.Commit(); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if err := tr2.Commit(); err == nil || err.Error() != "precondition failed" {
		t.Errorf("Expected precondition failed, got %q", err)
	}

	tr3 := &Transaction{}
	if x, err := o.Get(rootPublicID, tr3); err != nil || x.Stat.ID != s1.ID {
		t.Errorf("Value should exist: %v, %s", x, err)
	}
}

type Foo struct{}

func newFoo() *Foo {
	return &Foo{}
}

func getFoo(t *testing.T, st *Store, tr *Transaction, path string) *Foo {
	_, file, line, _ := runtime.Caller(1)
	v := get(t, st, tr, path)
	res, ok := v.(*Foo)
	if !ok {
		t.Fatalf("%s(%d): %s: not a Foo: %v", file, line, path, v)
	}
	return res
}

func TestSimpleMove(t *testing.T) {
	st, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Create / and /x.
	{
		tr := &Transaction{}
		put(t, st, tr, "/", newFoo())
		put(t, st, tr, "/x", newFoo())
		commit(t, tr)
	}

	// Move /x to /y.
	{
		tr := &Transaction{}
		x := getFoo(t, st, tr, "/x")
		remove(t, st, tr, "/x")
		put(t, st, tr, "/y", x)
		commit(t, tr)
	}
}

// Test a path conflict where some directory along the path to a value has been
// mutated concurrently.
func TestPathConflict(t *testing.T) {
	st, err := New(rootPublicID, "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	{
		tr := &Transaction{}
		put(t, st, tr, "/", newFoo())
		put(t, st, tr, "/a", newFoo())
		put(t, st, tr, "/a/b", newFoo())
		put(t, st, tr, "/a/b/c", newFoo())
		commit(t, tr)
	}

	// Add a new value.
	tr1 := &Transaction{}
	put(t, st, tr1, "/a/b/c/d", newFoo())

	// Change a directory along the path.
	tr2 := &Transaction{}
	put(t, st, tr2, "/a/b", newFoo())
	commit(t, tr2)

	// First Transaction should abort.
	if err := tr1.Commit(); err == nil {
		t.Errorf("Expected transaction to abort")
	}
}
