package test

import (
	"runtime"
	"testing"

	"veyron2/rt"
	"veyron2/storage"
)

func get(t *testing.T, st storage.Store, tr storage.Transaction, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rt.R().NewContext(), tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, st storage.Store, tr storage.Transaction, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	stat, err := st.Bind(path).Put(rt.R().NewContext(), tr, v)
	if err != nil || !stat.ID.IsValid() {
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	return stat.ID
}

func remove(t *testing.T, st storage.Store, tr storage.Transaction, path string) {
	if err := st.Bind(path).Remove(rt.R().NewContext(), tr); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func commit(t *testing.T, tr storage.Transaction) {
	if err := tr.Commit(rt.R().NewContext()); err != nil {
		t.Fatalf("Transaction aborted: %s", err)
	}
}
