// Modeled after veyron2/storage/vstore/blackbox/photoalbum_test.go.
//
// TODO(sadovsky): Maybe migrate this to be part of the public store API, to
// help with writing tests that use storage.

package state

import (
	"runtime"
	"testing"

	"veyron2/security"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
)

func get(t *testing.T, sn *MutableSnapshot, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	name := storage.ParsePath(path)
	e, err := sn.Get(rootPublicID, name)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, sn *MutableSnapshot, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	name := storage.ParsePath(path)
	stat, err := sn.Put(rootPublicID, name, v)
	if err != nil {
		t.Errorf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	if _, err := sn.Get(rootPublicID, name); err != nil {
		t.Errorf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	if stat != nil {
		return stat.ID
	}
	return storage.ID{}
}

func remove(t *testing.T, sn *MutableSnapshot, path string) {
	name := storage.ParsePath(path)
	if err := sn.Remove(rootPublicID, name); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}
