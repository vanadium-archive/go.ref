package query

import (
	"runtime"
	"testing"

	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
)

type Dir struct{}

func mkdir(t *testing.T, sn *state.MutableSnapshot, name string) (storage.ID, interface{}) {
	_, file, line, _ := runtime.Caller(1)
	dir := &Dir{}
	path := storage.ParsePath(name)
	stat, err := sn.Put(rootPublicID, path, dir)
	if err != nil || stat == nil {
		t.Errorf("%s(%d): mkdir %s: %s", file, line, name, err)
	}
	return stat.ID, dir
}

func get(t *testing.T, sn *state.MutableSnapshot, name string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	path := storage.ParsePath(name)
	e, err := sn.Get(rootPublicID, path)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, name, err)
	}
	return e.Value
}

func put(t *testing.T, sn *state.MutableSnapshot, name string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	path := storage.ParsePath(name)
	stat, err := sn.Put(rootPublicID, path, v)
	if err != nil {
		t.Errorf("%s(%d): can't put %s: %s", file, line, name, err)
	}
	if _, err := sn.Get(rootPublicID, path); err != nil {
		t.Errorf("%s(%d): can't get %s: %s", file, line, name, err)
	}
	if stat != nil {
		return stat.ID
	}
	return storage.ID{}
}

func remove(t *testing.T, sn *state.MutableSnapshot, name string) {
	path := storage.ParsePath(name)
	if err := sn.Remove(rootPublicID, path); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't remove %s: %s", file, line, name, err)
	}
}

func commit(t *testing.T, st *state.State, sn *state.MutableSnapshot) {
	if err := st.ApplyMutations(sn.Mutations()); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): commit failed: %s", file, line, err)
	}
}

func expectExists(t *testing.T, sn *state.MutableSnapshot, name string) {
	_, file, line, _ := runtime.Caller(1)
	path := storage.ParsePath(name)
	if _, err := sn.Get(rootPublicID, path); err != nil {
		t.Errorf("%s(%d): does not exist: %s", file, line, path)
	}
}

func expectNotExists(t *testing.T, sn *state.MutableSnapshot, name string) {
	path := storage.ParsePath(name)
	if _, err := sn.Get(rootPublicID, path); err == nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): should not exist: %s", file, line, name)
	}
}
