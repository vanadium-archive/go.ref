package state_test

import (
	"runtime"
	"testing"

	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/storage"
)

type Node struct {
	E map[string]storage.ID
}

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
	johnPublicID security.PublicID = security.FakePublicID("john")
	janePublicID security.PublicID = security.FakePublicID("jane")
	joanPublicID security.PublicID = security.FakePublicID("joan")

	rootUser = security.PrincipalPattern("fake/root")
	johnUser = security.PrincipalPattern("fake/john")
	janeUser = security.PrincipalPattern("fake/jane")
	joanUser = security.PrincipalPattern("fake/joan")
)

// makeParentNodes creates the parent nodes if they do not already exist.
func makeParentNodes(t *testing.T, sn *state.MutableSnapshot, user security.PublicID, path string) {
	_, file, line, _ := runtime.Caller(2)
	pl := storage.ParsePath(path)
	for i := 0; i < len(pl); i++ {
		name := pl[:i]
		if _, err := sn.Get(user, name); err != nil {
			if _, err := sn.Put(user, name, &Node{}); err != nil {
				t.Fatalf("%s(%d): can't put %s: %s", file, line, name, err)
			}
		}
	}
}

// mkdir creates an empty directory.  Creates the parent directories if they do
// not already exist.
func mkdir(t *testing.T, sn *state.MutableSnapshot, user security.PublicID, path string) storage.ID {
	makeParentNodes(t, sn, user, path)
	name := storage.ParsePath(path)
	stat, err := sn.Put(user, name, &Node{})
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	return stat.ID
}

// link adds a hard link to another store value.  Creates the parent directories
// if they do not already exist.
func link(t *testing.T, sn *state.MutableSnapshot, user security.PublicID, path string, id storage.ID) {
	makeParentNodes(t, sn, user, path)
	name := storage.ParsePath(path)
	if _, err := sn.Put(user, name, id); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
}

// putPath adds a value to the store.  Creates the parent directories if they do
// not already exist.
func putPath(t *testing.T, sn *state.MutableSnapshot, user security.PublicID, path string, v interface{}) storage.ID {
	makeParentNodes(t, sn, user, path)
	name := storage.ParsePath(path)
	stat, err := sn.Put(user, name, v)
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	return stat.ID
}

// put adds a value to the store.
func put(t *testing.T, sn *state.MutableSnapshot, user security.PublicID, path string, v interface{}) storage.ID {
	name := storage.ParsePath(path)
	stat, err := sn.Put(user, name, v)
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	return stat.ID
}

// maybePut tries to add a value to the store.
func maybePut(sn *state.MutableSnapshot, user security.PublicID, path string, v interface{}) (storage.ID, error) {
	name := storage.ParsePath(path)
	stat, err := sn.Put(user, name, v)
	if err != nil {
		return storage.ID{}, err
	}
	return stat.ID, nil
}

// maybeGet try to fetch a value from the store.
func maybeGet(sn *state.MutableSnapshot, user security.PublicID, path string) (interface{}, error) {
	name := storage.ParsePath(path)
	e, err := sn.Get(user, name)
	if err != nil {
		return nil, err
	}
	return e.Value, nil
}

// commit commits the state.
func commit(t *testing.T, st *state.State, sn *state.MutableSnapshot) {
	if err := st.ApplyMutations(sn.Mutations()); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't commit: %s", file, line, err)
	}
}

// Simple DAG based on teams and players.
func TestState(t *testing.T) {
	st := state.New(rootPublicID)

	// Add a Node under /a/b/c.
	sn1 := st.MutableSnapshot()
	nid := putPath(t, sn1, rootPublicID, "/a/b/c", &Node{})
	commit(t, st, sn1)

	// Create a link.
	{
		sn := st.MutableSnapshot()
		link(t, sn, rootPublicID, "/a/b/d", nid)
		commit(t, st, sn)
	}

	// Add a shared Entry.
	{
		sn := st.MutableSnapshot()
		mkdir(t, sn, rootPublicID, "/a/b/c/Entries/foo")
		commit(t, st, sn)
	}

	// Check that it exists.
	{
		sn := st.MutableSnapshot()
		if _, err := maybeGet(sn, rootPublicID, "/a/b/d/Entries/foo"); err != nil {
			t.Errorf("entry should exist")
		}
	}
}
