package memstore

import (
	"runtime"
	"testing"

	"veyron2/storage"
)

func mkdir(t *testing.T, st *Store, tr *Transaction, path string) (storage.ID, interface{}) {
	_, file, line, _ := runtime.Caller(1)
	dir := &Dir{}
	stat, err := st.Bind(path).Put(rootPublicID, tr, dir)
	if err != nil || stat == nil {
		t.Errorf("%s(%d): mkdir %s: %s", file, line, path, err)
	}
	return stat.ID, dir
}

func get(t *testing.T, st *Store, tr *Transaction, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, st *Store, tr *Transaction, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	stat, err := st.Bind(path).Put(rootPublicID, tr, v)
	if err != nil {
		t.Errorf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	if _, err := st.Bind(path).Get(rootPublicID, tr); err != nil {
		t.Errorf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	if stat != nil {
		return stat.ID
	}
	return storage.ID{}
}

func remove(t *testing.T, st *Store, tr *Transaction, path string) {
	if err := st.Bind(path).Remove(rootPublicID, tr); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func commit(t *testing.T, tr *Transaction) {
	if err := tr.Commit(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): Transaction aborted: %s", file, line, err)
	}
}

func expectExists(t *testing.T, st *Store, tr *Transaction, path string) {
	_, file, line, _ := runtime.Caller(1)
	if ok, _ := st.Bind(path).Exists(rootPublicID, tr); !ok {
		t.Errorf("%s(%d): does not exist: %s", file, line, path)
	}
}

func expectNotExists(t *testing.T, st *Store, tr *Transaction, path string) {
	if e, err := st.Bind(path).Get(rootPublicID, tr); err == nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): should not exist: %s: got %+v", file, line, path, e.Value)
	}
}

func expectValue(t *testing.T, st *Store, tr *Transaction, path string, v interface{}) {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Errorf("%s(%d): does not exist: %s", file, line, path)
		return
	}
	if e.Value != v {
		t.Errorf("%s(%d): expected %+v, got %+v", file, line, e.Value, v)
	}

}
