package follow

import (
	"os"
	"testing"
	"time"
)

func TestModificationStat(t *testing.T) {
	testFileName := os.TempDir() + "/follow.modification.stat"
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	defer testfile.Close()
	defer os.Remove(testFileName)

	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed : %v", err)
	}
	timeout := 100 * time.Millisecond
	if err := testModification(testfile, watcher, timeout); err != nil {
		t.Fatalf("testModification() failed: %v", err)
	}
}
