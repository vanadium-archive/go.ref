// +build darwin freebsd linux netbsd openbsd windows

package follow

import (
	"os"
	"testing"
	"time"
)

func TestModificationNotify(t *testing.T) {
	testFileName := os.TempDir() + "/follow.modification.notify"
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	defer testfile.Close()
	defer os.Remove(testFileName)

	watcher, err := newFSNotifyWatcher(testFileName)
	if err != nil {
		t.Fatalf("newCustomFSWatcer() failed: %v", err)
	}
	timeout := time.Second
	if err := testModification(testfile, watcher, timeout); err != nil {
		t.Fatalf("testModification() failed: %v", err)
	}
}
