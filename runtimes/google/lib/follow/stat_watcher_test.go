package follow

import (
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func TestModificationStat(t *testing.T) {
	// Create the test file.
	testFile, err := ioutil.TempFile(os.TempDir(), "follow.modification.stat")
	if err != nil {
		t.Fatalf("ioutil.TempFile() failed: %v", err)
	}
	defer testFile.Close()
	testFileName := testFile.Name()
	defer os.Remove(testFileName)

	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed : %v", err)
	}
	timeout := 100 * time.Millisecond
	if err := testModification(testFile, watcher, timeout); err != nil {
		t.Fatalf("testModification() failed: %v", err)
	}
}
