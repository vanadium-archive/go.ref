package follow

import (
	"io"
	"os"
	"sync"
)

// fsReader is an implementation of io.ReadCloser that reads synchronously
// from a file, blocking until at least one byte is written to the file and is
// available for reading.
type fsReader struct {
	mu sync.Mutex
	// The file to read.
	file *os.File // GUARDED_BY(mu)
	// The watcher of modifications to the file.
	watcher fsWatcher
	// True if the reader is open for reading, false otherwise.
	closed bool // GUARDED_BY(mu)
}

// NewReader creates a new reader that reads synchronously from a file,
// blocking until at least one byte is written to the file and is available
// for reading.
// The returned io.ReadCloser supports limited concurrency:
// 1) Reads may not be called concurrently.
// 2) Close may be called concurrently with Read, and will terminate Read.
func NewReader(filename string) (reader io.ReadCloser, err error) {
	var file *os.File
	var watcher fsWatcher
	defer func() {
		if err == nil {
			return
		}
		var closeFileErr, closeWatcherErr error
		if file != nil {
			closeFileErr = file.Close()
		}
		if watcher != nil {
			closeWatcherErr = watcher.Close()
		}
		err = composeErrors(err, closeFileErr, closeWatcherErr)
	}()
	file, err = os.Open(filename)
	if err != nil {
		return nil, err
	}
	watcher, err = newFSWatcher(filename)
	if err != nil {
		return nil, err
	}
	return newCustomReader(file, watcher)
}

func newCustomReader(file *os.File, watcher fsWatcher) (io.ReadCloser, error) {
	reader := &fsReader{
		file:    file,
		watcher: watcher,
	}
	return reader, nil
}

func (r *fsReader) Read(p []byte) (int, error) {
	// If the reader has been closed, return an error.
	r.mu.Lock()
	if r.closed {
		return 0, io.EOF
	}

	for {
		// Read any bytes that are available.
		if n, err := r.file.Read(p); err != io.EOF {
			r.mu.Unlock()
			return n, err
		}
		r.mu.Unlock()

		// Wait until the file is modified one or more times. The new
		// bytes from each corresponding modification have been
		// written to the file already, and therefore won't be skipped.
		if err := r.watcher.Wait(); err != nil {
			return 0, err
		}

		r.mu.Lock()
	}
}

// Close closes the reader synchronously.
// 1) Terminates ongoing reads. (reads return io.EOF)
// 2) Prevents future reads. (reads return io.EOF)
// 3) Frees system resources associated with the reader.
// Close is idempotent.
func (r *fsReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	// Mark the reader closed.
	r.closed = true
	// Release resources.
	closeFileErr := r.file.Close()
	closeWatcherErr := r.watcher.Close()
	return composeErrors(closeFileErr, closeWatcherErr)
}
