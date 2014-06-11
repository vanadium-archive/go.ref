package state

import (
	"fmt"
	"runtime"
	"testing"

	"veyron/services/store/memstore/refs"

	"veyron2/storage"
	"veyron2/vom"
)

// Dir is a simple directory.
type Dir struct {
	Entries map[string]storage.ID
}

var (
	root     = &Dir{}
	rootPath = storage.ParsePath("/")
)

func init() {
	vom.Register(&Dir{})
}

func mkdir(t *testing.T, sn *MutableSnapshot, path string) (storage.ID, interface{}) {
	_, file, line, _ := runtime.Caller(1)
	dir := &Dir{}
	stat, err := sn.Put(rootPublicID, storage.ParsePath(path), dir)
	if err != nil || stat == nil {
		t.Errorf("%s(%d): mkdir %s: %s", file, line, path, err)
		return storage.ID{}, dir
	}
	m, ok := sn.mutations.Delta[stat.ID]
	if !ok {
		t.Errorf("%s(%d): Expected Mutation: %v %v", file, line, stat, sn.mutations)
	} else if _, ok := m.Value.(*Dir); !ok {
		t.Fatalf("%s(%d): %s: not a directory: %v -> %v", file, line, path, stat, m.Value)
	}
	return stat.ID, dir
}

func expectExists(t *testing.T, sn *MutableSnapshot, id storage.ID) {
	_, file, line, _ := runtime.Caller(1)
	if !sn.idTable.Contains(&Cell{ID: id}) {
		t.Errorf("%s(%d): does not exist: %s", file, line, id)
	}
	if _, err := sn.Get(rootPublicID, storage.ParsePath(fmt.Sprintf("/uid/%s", id))); err != nil {
		t.Errorf("%s(%d): does not exist: %s", file, line, id)
	}
}

func expectNotExists(t *testing.T, sn *MutableSnapshot, id storage.ID) {
	if sn.idTable.Contains(&Cell{ID: id}) {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): should not exist: %s", file, line, id)
	}
}

func expectValue(t *testing.T, sn *MutableSnapshot, path string, v interface{}) {
	_, file, line, _ := runtime.Caller(1)
	cell, _, _ := sn.resolveCell(sn.newPermChecker(rootPublicID), storage.ParsePath(path), nil)
	if cell == nil {
		t.Errorf("%s(%d): path does not exist: %s", file, line, path)
	}
	if cell.Value == nil {
		t.Errorf("%s(%d): cell has a nil value: %s", file, line, path)
	}
}

func checkInRefs(t *testing.T, sn *MutableSnapshot) {
	_, file, line, _ := runtime.Caller(1)

	sn.idTable.Iter(func(it interface{}) bool {
		c1 := it.(*Cell)

		// Check that each out-ref has an in-ref.
		c1.refs.Iter(func(it interface{}) bool {
			r := it.(*refs.Ref)
			c2 := sn.Find(r.ID)
			if c2 == nil {
				t.Errorf("%s(%d): dangling reference: %s", file, line, r.ID)
			} else if !c2.inRefs.Contains(&refs.Ref{ID: c1.ID, Path: r.Path}) {
				t.Errorf("%s(%d): inRef does not exist: %s <- %s", file, line, c1.ID, c2.ID)
			}
			return true
		})

		// Check that each in-ref has an out-ref.
		c1.inRefs.Iter(func(it interface{}) bool {
			r := it.(*refs.Ref)
			c2 := sn.Find(r.ID)
			if c2 == nil {
				t.Errorf("%s(%d): dangling reference: %s", file, line, r.ID)
			} else if !c2.refs.Contains(&refs.Ref{ID: c1.ID, Path: r.Path}) {
				t.Errorf("%s(%d): inRef does not exist: %s -> %s", file, line, c2.ID, c1.ID)
			}
			return true
		})
		return true
	})
}

// Set up a root directory.
func TestRoot(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)

	// There should be no root.
	v, err := sn.Get(rootPublicID, rootPath)
	if v != nil {
		t.Errorf("Expected nil for /: %v", v)
	}
	if err == nil {
		t.Errorf("Expected error")
	}

	// Add the root object.
	stat, err := sn.Put(rootPublicID, rootPath, root)
	if err != nil {
		t.Errorf("Error adding root: %s", err)
	}
	if sn.mutations.RootID != stat.ID {
		t.Errorf("Expected root update")
	}
	{
		p, ok := sn.mutations.Preconditions[sn.mutations.RootID]
		if !ok {
			t.Errorf("Error fetching root")
		}
		if p != 0 {
			t.Errorf("Expected 0 precondition: %d", p)
		}
	}

	// Fetch the root object, and compare.
	v, err = sn.Get(rootPublicID, rootPath)
	if err != nil {
		t.Errorf("Error fetching root: %s", err)
	}

	checkInRefs(t, sn)
}

// Make a directory tree.
func TestDirTree(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)

	id1, d1 := mkdir(t, sn, "/")
	id2, d2 := mkdir(t, sn, "/Entries/a")
	id3, d3 := mkdir(t, sn, "/Entries/a/Entries/b")
	id4, d4 := mkdir(t, sn, "/Entries/a/Entries/b/Entries/c")
	id5, d5 := mkdir(t, sn, "/Entries/a/Entries/b/Entries/d")
	expectExists(t, sn, id1)
	expectExists(t, sn, id2)
	expectExists(t, sn, id3)
	expectExists(t, sn, id4)
	expectExists(t, sn, id5)

	// Parent directory has to exist.
	d := &Dir{}
	if _, err := sn.Put(rootPublicID, storage.ParsePath("/a/c/e"), d); err == nil {
		t.Errorf("Expected error")
	}

	expectValue(t, sn, "/", d1)
	expectValue(t, sn, "/Entries/a", d2)
	expectValue(t, sn, "/Entries/a/Entries/b", d3)
	expectValue(t, sn, "/Entries/a/Entries/b/Entries/c", d4)
	expectValue(t, sn, "/Entries/a/Entries/b/Entries/d", d5)
	checkInRefs(t, sn)

	// Remove part of the tree.
	if err := sn.Remove(rootPublicID, storage.ParsePath("/Entries/a/Entries/b")); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	sn.gc()
	expectExists(t, sn, id1)
	expectExists(t, sn, id2)
	expectNotExists(t, sn, id3)
	expectNotExists(t, sn, id4)
	expectNotExists(t, sn, id5)
	checkInRefs(t, sn)
}

// Make some references.
func TestRef(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)

	rootID, _ := mkdir(t, sn, "/")
	ePath := storage.ParsePath("/Entries/a")

	// Not possible to create a Dir with a dangling reference.
	d := &Dir{Entries: map[string]storage.ID{"ref": storage.NewID()}}
	if _, err := sn.Put(rootPublicID, ePath, d); err != ErrBadRef {
		t.Errorf("Error should be %q: got %q", ErrBadRef, err)
	}

	// Set the Ref to refer to the root.
	d.Entries["ref"] = rootID
	stat, err := sn.Put(rootPublicID, ePath, d)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectExists(t, sn, stat.ID)
	checkInRefs(t, sn)

	// Change the ref to refer to itself.
	d.Entries["ref"] = stat.ID
	if stat2, err := sn.Put(rootPublicID, ePath, d); err != nil || stat2.ID != stat.ID {
		t.Errorf("Unexpected error: %s", err)
	}
	sn.gc()
	expectExists(t, sn, stat.ID)
	checkInRefs(t, sn)

	// Remove it.
	if err := sn.Remove(rootPublicID, ePath); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	sn.gc()
	expectNotExists(t, sn, stat.ID)
	checkInRefs(t, sn)
}

// Make an implicit directory tree.
func TestImplicitDirTree(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)

	id1, d1 := mkdir(t, sn, "/")
	id2, d2 := mkdir(t, sn, "/a")
	id3, d3 := mkdir(t, sn, "/a/b")
	id4, d4 := mkdir(t, sn, "/a/b/c")
	id5, d5 := mkdir(t, sn, "/a/b/c/d")
	expectExists(t, sn, id1)
	expectExists(t, sn, id2)
	expectExists(t, sn, id3)
	expectExists(t, sn, id4)
	expectExists(t, sn, id5)
	checkInRefs(t, sn)

	// Parent directory has to exisn.
	d := &Dir{}
	if _, err := sn.Put(rootPublicID, storage.ParsePath("/a/c/e"), d); err == nil {
		t.Errorf("Expected error")
	}

	expectValue(t, sn, "/", d1)
	expectValue(t, sn, "/a", d2)
	expectValue(t, sn, "/a/b", d3)
	expectValue(t, sn, "/a/b/c", d4)
	expectValue(t, sn, "/a/b/c/d", d5)
	checkInRefs(t, sn)

	// Remove part of the tree.
	if err := sn.Remove(rootPublicID, storage.ParsePath("/a/b")); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	sn.gc()
	expectExists(t, sn, id1)
	expectExists(t, sn, id2)
	expectNotExists(t, sn, id3)
	expectNotExists(t, sn, id4)
	expectNotExists(t, sn, id5)
	checkInRefs(t, sn)
}

// Tests that nil maps are converted to empty maps.
func TestPutToNilMap(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)

	var m map[string]interface{}
	if _, err := sn.Put(rootPublicID, storage.PathName{}, m); err != nil {
		t.Error("failure during nil map put: ", err)
	}
	if _, err := sn.Put(rootPublicID, storage.PathName{"z"}, "z"); err != nil {
		t.Error("failure during put of child of nil map: ", err)
	}
}

// Tests that slices are settable so that we can append.
func TestAppendToSlice(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)
	if _, err := sn.Put(rootPublicID, storage.ParsePath("/"), []int{}); err != nil {
		t.Error("failure during put of empty slice: ", err)
	}
	if _, err := sn.Put(rootPublicID, storage.ParsePath("/@"), 1); err != nil {
		t.Error("failure during append to slice: ", err)
	}
}
