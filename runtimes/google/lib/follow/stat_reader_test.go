package follow

import (
	"io/ioutil"
	"os"
	"testing"
	"time"
)

// TestStatReadPartial tests partial reads with the os.Stat()-based fsReader
func TestStatReadPartial(t *testing.T) {
	// Create the test file.
	testFile, err := ioutil.TempFile(os.TempDir(), "follow.reader.stat.partial")
	if err != nil {
		t.Fatalf("ioutil.TempFile() failed: %v", err)
	}
	testFile.Close()
	testFileName := testFile.Name()
	defer os.Remove(testFileName)

	// Create the os.Stat()-based fsWatcher.
	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := time.Second
	if err := testReadPartial(testFileName, watcher, timeout); err != nil {
		t.Fatalf("testReadPartial() failed: %v", err)
	}
}

// TestStatReadFull tests full reads with the os.Stat()-based fsReader
func TestStatReadFull(t *testing.T) {
	// Create the test file.
	testFile, err := ioutil.TempFile(os.TempDir(), "follow.reader.stat.full")
	if err != nil {
		t.Fatalf("ioutil.TempFile() failed: %v", err)
	}
	testFile.Close()
	testFileName := testFile.Name()
	defer os.Remove(testFileName)

	// Create the os.Stat()-based fsWatcher.
	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := time.Second
	if err := testReadFull(testFileName, watcher, timeout); err != nil {
		t.Fatalf("testReadFull() failed: %v", err)
	}
}

// TestStatClose tests close with the os.Stat()-based fsReader
func TestStatClose(t *testing.T) {
	// Create the test file.
	testFile, err := ioutil.TempFile(os.TempDir(), "follow.reader.stat.close")
	if err != nil {
		t.Fatalf("ioutil.TempFile() failed: %v", err)
	}
	testFile.Close()
	testFileName := testFile.Name()
	defer os.Remove(testFileName)

	// Create the os.Stat()-based fsWatcher.
	minSleep := 10 * time.Millisecond
	maxSleep := 100 * time.Millisecond
	watcher, err := newCustomFSStatWatcher(testFileName, minSleep, maxSleep)
	if err != nil {
		t.Fatalf("newCustomFSWatcher() failed: %v", err)
	}

	timeout := time.Second
	if err := testClose(testFileName, watcher, timeout); err != nil {
		t.Fatalf("testClose() failed: %v", err)
	}
}
