package follow

import (
	"os"
	"testing"
	"time"
)

func TestModificationTimed(t *testing.T) {
	testFileName := os.TempDir() + "/follow.modification.timed"
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	defer testfile.Close()
	defer os.Remove(testFileName)

	sleep := 10 * time.Millisecond
	watch := newCustomFSTimedWatch(testFileName, sleep)
	watcher, err := newCustomFSWatcher(watch)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed : %v", err)
	}
	timeout := 100 * time.Millisecond
	// don't modify file, expect event.
	if err := expectModification(watcher.events, timeout); err != nil {
		t.Fatalf("expectModification() failed without a file modification: %v", err)
	}
	// modify file, expect event.
	if err := writeString(testfile, "modification"); err != nil {
		t.Fatalf("writeString() failed: %v", err)
	}
	if err := expectModification(watcher.events, timeout); err != nil {
		t.Fatalf("expectModification() failed after a file modification: %v", err)
	}
}
