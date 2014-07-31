package test

import (
	"runtime"
	"testing"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/storage"
)

func createTransaction(t *testing.T, st storage.Store, name string) string {
	_, file, line, _ := runtime.Caller(1)
	tid, err := st.BindTransactionRoot(name).CreateTransaction(rt.R().NewContext())
	if err != nil {
		t.Fatalf("%s(%d): can't create transaction %s: %s", file, line, name, err)
	}
	return naming.Join(name, tid)
}

func get(t *testing.T, st storage.Store, name string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.BindObject(name).Get(rt.R().NewContext())
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, name, err)
	}
	return e.Value
}

func put(t *testing.T, st storage.Store, name string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	stat, err := st.BindObject(name).Put(rt.R().NewContext(), v)
	if err != nil || !stat.ID.IsValid() {
		t.Fatalf("%s(%d): can't put %s: %s", file, line, name, err)
	}
	return stat.ID
}

func remove(t *testing.T, st storage.Store, name string) {
	_, file, line, _ := runtime.Caller(1)
	if err := st.BindObject(name).Remove(rt.R().NewContext()); err != nil {
		t.Errorf("%s(%d): can't remove %s: %s", file, line, name, err)
	}
}

func commit(t *testing.T, st storage.Store, name string) {
	_, file, line, _ := runtime.Caller(1)
	if err := st.BindTransaction(name).Commit(rt.R().NewContext()); err != nil {
		t.Fatalf("%s(%d): commit failed: %s", file, line, err)
	}
}
