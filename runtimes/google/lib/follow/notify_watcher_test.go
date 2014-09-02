// +build darwin freebsd linux netbsd openbsd windows

package follow

import (
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func TestModificationNotify(t *testing.T) {
	// Create the test file.
	testFile, err := ioutil.TempFile(os.TempDir(), "follow.modification.notify")
	if err != nil {
		t.Fatalf("ioutil.TempFile() failed: %v", err)
	}
	defer testFile.Close()
	testFileName := testFile.Name()
	defer os.Remove(testFileName)

	watcher, err := newFSNotifyWatcher(testFileName)
	if err != nil {
		t.Fatalf("newCustomFSWatcer() failed: %v", err)
	}
	timeout := time.Second
	if err := testModification(testFile, watcher, timeout); err != nil {
		t.Fatalf("testModification() failed: %v", err)
	}
}
