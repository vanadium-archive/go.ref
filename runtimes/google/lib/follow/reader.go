package follow

import (
	"errors"
	"io"
	"os"
	"sync"
)

var errClosed = errors.New("reader has already been closed")

// fsReader is an implementation of io.ReadCloser that reads synchronously
// from a file, blocking until at least one byte is written to the file and is
// available for reading.
// fsReader should not be accessed concurrently.
type fsReader struct {
	// The file to read.
	file *os.File
	// The watcher of modifications to the file.
	watcher *fsWatcher
	// mu guards closed
	mu sync.Mutex
	// True if the reader is open for reading, false otherwise.
	closed bool // GUARDED_BY(mu)
}

// NewReader creates a new reader that reads synchronously from a file,
// blocking until at least one byte is written to the file and is available
// for reading.
// The returned ReadCloser should not be accessed concurrently.
func NewReader(file *os.File) (io.ReadCloser, error) {
	watcher, err := newFSWatcher(file.Name())
	if err != nil {
		return nil, err
	}
	return newCustomReader(file, watcher)
}

func newCustomReader(file *os.File, watcher *fsWatcher) (io.ReadCloser, error) {
	reader := &fsReader{
		file:    file,
		watcher: watcher,
	}
	return reader, nil
}

func (r *fsReader) Read(p []byte) (int, error) {
	// If the reader has been closed, return an error.
	if r.isClosed() {
		return 0, io.EOF
	}

	for {
		// Attempt to read enough bytes to fill the buffer.
		if n, err := r.file.Read(p); err != io.EOF {
			return n, err
		}
		// Wait until the file is modified one or more times. The new
		// bytes from each corresponding modification have been
		// written to the file already, and therefore won't be skipped.
		if err := receiveEvents(r.watcher.events); err != nil {
			return 0, err
		}
	}
}

// receiveEvents receives events from an event channel, blocking until at
// least one event is available. It also attempts to drain the channel without
// blocking, as long as nil events are immediately available.
// However, if an error is encountered, it is returned immediately with no
// further attempt to drain the channel.
// io.EOF is returned if the event channel is closed (as a result of Close()).
func receiveEvents(events <-chan error) error {
	err, ok := <-events
	if err != nil {
		return err
	}
	if !ok {
		return io.EOF
	}
	// TODO(tilaks): De-duplicate on send.
	for {
		select {
		case err, ok := <-events:
			if err != nil {
				return err
			}
			if !ok {
				return io.EOF
			}
			// Attempt to deduplicate following events.
		default:
			// No events more events to deduplicate.
			return nil
		}
	}
}

// Close closes the reader synchronously.
// 1) Terminates ongoing reads. (reads return io.EOF)
// 2) Prevents future reads. (reads return io.EOF)
// 3) Frees system resources associated with the reader.
func (r *fsReader) Close() error {
	// Mark the reader closed.
	if err := r.setClosed(); err != nil {
		return err
	}

	// Release resources.
	closeFileErr := r.file.Close()
	closeWatcherErr := r.watcher.Close()
	return composeErrors(closeFileErr, closeWatcherErr)
}

func (r *fsReader) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *fsReader) setClosed() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	r.closed = true
	return nil
}
