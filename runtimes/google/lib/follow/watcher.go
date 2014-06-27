package follow

// fsWatcher is a tool for watching append-only modifications to a file.
type fsWatcher interface {
	// Wait blocks until the file is modified.
	// Wait returns an io.EOF if the watcher is closed, and immediately returns
	// any error it encounters while blocking.
	Wait() error

	// Close closes the watcher synchronously. Any ongoing or following calls
	// to Wait return io.EOF.
	// Close is idempotent.
	Close() error
}
