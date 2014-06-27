// +build darwin freebsd linux netbsd openbsd windows

package follow

import (
	"os"
	"testing"
	"time"
)

// TestNotifyReadPartial tests partial reads with the fsnotify-based fsReader
func TestNotifyReadPartial(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.notify.partial"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the fsnotify-based fsWatcher.
	watcher, err := newFSNotifyWatcher(testFileName)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := time.Second
	if err := testReadPartial(testFileName, watcher, timeout); err != nil {
		t.Fatal("testReadPartial() failed: %v", err)
	}
}

// TestNotifyReadFull tests full reads with the fsnotify-based fsReader
func TestNotifyReadFull(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.notify.full"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the fsnotify-based fsWatcher.
	watcher, err := newFSNotifyWatcher(testFileName)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := time.Second
	if err := testReadFull(testFileName, watcher, timeout); err != nil {
		t.Fatal("testReadFull() failed: %v", err)
	}
}

// TestNotifyClose tests close with the fsnotify-based fsReader
func TestNotifyClose(t *testing.T) {
	testFileName := os.TempDir() + "/follow.reader.notify.close"

	// Create the test file.
	testfile, err := os.Create(testFileName)
	if err != nil {
		t.Fatalf("os.Create() failed: %v", err)
	}
	testfile.Close()
	defer os.Remove(testFileName)

	// Create the fsnotify-based fsWatcher.
	watcher, err := newFSNotifyWatcher(testFileName)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := time.Second
	if err := testClose(testFileName, watcher, timeout); err != nil {
		t.Fatal("testClose() failed: %v", err)
	}
}
