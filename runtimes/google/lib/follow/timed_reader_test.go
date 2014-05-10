package follow

import (
	"os"
	"testing"
	"time"
)

// TestTimedReadPartial tests partial reads with the timer-based fsReader
func TestTimedReadPartial(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.timed.partial"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the timer-based fsWatcher.
	sleep := 10 * time.Millisecond
	watch := newCustomFSTimedWatch(testFileName, sleep)
	watcher, err := newCustomFSWatcher(watch)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	if err := testReadPartial(testFileName, watcher, timeout); err != nil {
		t.Fatal("testReadPartial() failed: %v", err)
	}
}

// TestTimedReadFull tests full reads with the timer-based fsReader
func TestTimedReadFull(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.timed.full"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the timer-based fsWatcher.
	sleep := 10 * time.Millisecond
	watch := newCustomFSTimedWatch(testFileName, sleep)
	watcher, err := newCustomFSWatcher(watch)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	if err := testReadFull(testFileName, watcher, timeout); err != nil {
		t.Fatal("testReadFull() failed: %v", err)
	}
}

// TestTimedClose tests close with the timer-based fsReader
func TestTimedClose(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.timed.close"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the timer-based fsWatcher.
	sleep := 10 * time.Millisecond
	watch := newCustomFSTimedWatch(testFileName, sleep)
	watcher, err := newCustomFSWatcher(watch)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	if err := testClose(testFileName, watcher, timeout); err != nil {
		t.Fatal("testClose() failed: %v", err)
	}
}
