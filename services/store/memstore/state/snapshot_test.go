package state

import (
	"runtime"
	"testing"
	"veyron2/storage"
)

func expectExistsImmutable(t *testing.T, sn Snapshot, path string) {
	_, file, line, _ := runtime.Caller(1)
	if _, err := sn.Get(rootPublicID, storage.ParsePath(path)); err != nil {
		t.Errorf("%s(%d): does not exist: %s", file, line, path)
	}
}

func expectNotExistsImmutable(t *testing.T, sn Snapshot, path string) {
	_, file, line, _ := runtime.Caller(1)
	if _, err := sn.Get(rootPublicID, storage.ParsePath(path)); err == nil {
		t.Errorf("%s(%d): should not exist: %s", file, line, path)
	}
}

// TestImmutableGet is very similar to TestImplicitDirTree in
// mutable_snapshot_test.go except that it uses the immutable Snapshot type
// instead of MutableSnapshot.  The implementations of Get are different
// between the two types.
func TestImmutableGet(t *testing.T) {
	sn := newMutableSnapshot(rootPublicID)

	mkdir(t, sn, "/")
	mkdir(t, sn, "/Entries/a")
	mkdir(t, sn, "/Entries/a/Entries/b")
	mkdir(t, sn, "/Entries/a/Entries/b/Entries/c")
	mkdir(t, sn, "/Entries/a/Entries/b/Entries/d")
	expectExistsImmutable(t, &sn.snapshot, "/")
	expectExistsImmutable(t, &sn.snapshot, "/Entries/a")
	expectExistsImmutable(t, &sn.snapshot, "/Entries/a/Entries/b")
	expectExistsImmutable(t, &sn.snapshot, "/Entries/a/Entries/b/Entries/c")
	expectExistsImmutable(t, &sn.snapshot, "/Entries/a/Entries/b/Entries/d")

	// Remove part of the tree.
	if err := sn.Remove(rootPublicID, storage.ParsePath("/Entries/a/Entries/b")); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	sn.gc()
	expectExistsImmutable(t, &sn.snapshot, "/")
	expectExistsImmutable(t, &sn.snapshot, "/Entries/a")
	expectNotExistsImmutable(t, &sn.snapshot, "/Entries/a/Entries/b")
	expectNotExistsImmutable(t, &sn.snapshot, "/Entries/a/Entries/b/Entries/c")
	expectNotExistsImmutable(t, &sn.snapshot, "/Entries/a/Entries/b/Entries/d")
}
