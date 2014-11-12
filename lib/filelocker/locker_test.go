package filelocker

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
)

func init() {
	modules.RegisterChild("testLockUnlockChild", "", testLockUnlockChild)
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func newFile() string {
	file, err := ioutil.TempFile("", "test_lock_file")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	return file.Name()
}

func grabbedLock(lock <-chan bool) bool {
	select {
	case <-lock:
		return true
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

func testLockUnlockChild(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	// Lock the file
	unlocker, err := Lock(args[1])
	if err != nil {
		return fmt.Errorf("Lock failed: %v", err)
	}
	fmt.Fprintf(stdout, "locked\n")

	// Wait for message from parent to unlock the file.
	scanner := bufio.NewScanner(stdin)
	if scanned := scanner.Scan(); !scanned || (scanned && scanner.Text() != "unlock") {
		unlocker.Unlock()
		return fmt.Errorf("unexpected message read from stdout, expected %v", "unlock")
	}
	unlocker.Unlock()
	fmt.Fprintf(stdout, "unlocked\n")
	return nil
}

func TestLockUnlockInterProcess(t *testing.T) {
	filepath := newFile()
	defer os.Remove(filepath)

	sh := modules.NewShell("testLockUnlockChild")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start("testLockUnlockChild", nil, filepath)
	if err != nil {
		t.Fatalf("sh.Start failed: %v", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)

	// Test that child grabbed the lock.
	s.Expect("locked")

	// Test that parent cannot grab the lock, and then send a message
	// to the child to release the lock.
	lock := make(chan bool)
	go func() {
		unlocker, err := Lock(filepath)
		if err != nil {
			t.Fatalf("Lock failed: %v", err)
		}
		close(lock)
		unlocker.Unlock()
	}()
	if grabbedLock(lock) {
		t.Fatal("Parent process unexpectedly grabbed lock before child released it")
	}

	// Test that the parent can grab the lock after the child has released it.
	h.Stdin().Write([]byte("unlock\n"))
	s.Expect("unlocked")
	if !grabbedLock(lock) {
		t.Fatal("Parent process failed to grab the lock after child released it")
	}
	s.ExpectEOF()
}

func TestLockUnlockIntraProcess(t *testing.T) {
	filepath := newFile()
	defer os.Remove(filepath)

	// Grab the lock within this goroutine and test that
	// another goroutine blocks when trying to grab the lock.
	unlocker, err := Lock(filepath)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	lock := make(chan bool)
	go func() {
		unlocker, err := Lock(filepath)
		if err != nil {
			t.Fatalf("Lock failed: %v", err)
		}
		close(lock)
		unlocker.Unlock()
	}()
	if grabbedLock(lock) {
		unlocker.Unlock()
		t.Fatal("Another goroutine unexpectedly grabbed lock before this goroutine released it")
	}

	// Release the lock and test that the goroutine can grab it.
	unlocker.Unlock()
	if !grabbedLock(lock) {
		t.Fatal("Another goroutine failed to grab the lock after this goroutine released it")
	}
}
