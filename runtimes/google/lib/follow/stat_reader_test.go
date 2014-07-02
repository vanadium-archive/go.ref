package follow

import (
	"os"
	"testing"
	"time"
)

// TestStatReadPartial tests partial reads with the os.Stat()-based fsReader
func TestStatReadPartial(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.stat.partial"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the os.Stat()-based fsWatcher.
	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	if err := testReadPartial(testFileName, watcher, timeout); err != nil {
		t.Fatalf("testReadPartial() failed: %v", err)
	}
}

// TestStatReadFull tests full reads with the os.Stat()-based fsReader
func TestStatReadFull(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.stat.full"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the os.Stat()-based fsWatcher.
	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	if err := testReadFull(testFileName, watcher, timeout); err != nil {
		t.Fatalf("testReadFull() failed: %v", err)
	}
}

// TestStatClose tests close with the os.Stat()-based fsReader
func TestStatClose(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.stat.close"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the os.Stat()-based fsWatcher.
	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	if err := testClose(testFileName, watcher, timeout); err != nil {
		t.Fatalf("testClose() failed: %v", err)
	}
}
