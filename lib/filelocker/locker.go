// package filelocker provides a primitive that lets a process
// lock access to a file.
package filelocker

import (
	"os"
	"syscall"

	"veyron.io/veyron/veyron2/vlog"
)

// Unlocker is the interface to unlock a locked file.
type Unlocker interface {
	// Unlock unlocks the file.
	Unlock()
}

// Lock locks the provided file.
//
// If the file is already locked then the calling goroutine
// blocks until the lock can be acquired.
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

// unlocker implements Unlocker.
type unlocker struct {
	file *os.File
}

func (u *unlocker) Unlock() {
	if err := u.file.Close(); err != nil {
		vlog.Errorf("failed to unlock file %q: %v", u.file.Name(), err)
	}
}
