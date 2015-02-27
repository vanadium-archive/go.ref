// package filelocker provides a primitive that lets a process
// lock access to a file.
package filelocker

import (
	"os"
	"syscall"

	"v.io/x/lib/vlog"
)

// Unlocker is the interface to unlock a locked file.
type Unlocker interface {
	// Unlock unlocks the file.
	Unlock()
}

// Lock locks the provided file.
//
// If the file is already locked then the calling goroutine
// blocks until the lock can be grabbed.
//
// The file must exist otherwise an error is returned.
func Lock(filepath string) (Unlocker, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	return &unlocker{file}, nil
}

// TryLock tries to grab a lock on the provided file and
// returns an error if it fails. This function is non-blocking.
//
// The file must exist otherwise an error is returned.
func TryLock(filepath string) (Unlocker, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	return &unlocker{file}, nil
}

// unlocker implements Unlocker.
type unlocker struct {
	file *os.File
}

func (u *unlocker) Unlock() {
	if err := u.file.Close(); err != nil {
		vlog.Errorf("failed to unlock file %q: %v", u.file.Name(), err)
	}
}
