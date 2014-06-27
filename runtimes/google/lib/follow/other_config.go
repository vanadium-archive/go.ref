// +build !darwin,!freebsd,!linux,!netbsd,!openbsd,!windows

package follow

// newFSWatcher starts and returns a new os.Stat()-based fsWatcher.
// filename specifies the file to watch.
func newFSWatcher(filename string) (fsWatcher, error) {
	return newFSStatWatcher(filename)
}
