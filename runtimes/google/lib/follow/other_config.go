// +build !darwin,!freebsd,!linux,!netbsd,!openbsd,!windows

package follow

// newFSWatcher starts and returns a new timer-based fsWatcher.
// filename specifies the file to watch.
func newFSWatcher(filename string) (*fsWatcher, error) {
	watch := newFSTimedWatch(filename)
	return newCustomFSWatcher(watch)
}
