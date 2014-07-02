package follow

import (
	"io"
	"os"
	"sync"
	"time"

	vsync "veyron/runtimes/google/lib/sync"
)

const (
	defaultMinSleep = 10 * time.Millisecond
	defaultMaxSleep = 5 * time.Second
)

type fsStatWatcher struct {
	minSleep     time.Duration
	maxSleep     time.Duration
	file         *os.File
	lastFileSize int64
	// cancel signals Wait to terminate.
	cancel chan struct{}
	// pending allows Close to block till ongoing calls to Wait terminate.
	pending vsync.WaitGroup
	// mu and closed ensure that Close is idempotent.
	mu     sync.Mutex
	closed bool // GUARDED_BY(mu)
}

// newFSStatWatcher returns an fsWatcher that polls os.Stat(), observing file
// size. If the file size is larger than the previously-recorded file size,
// the watcher assumes the file has been modified.
// Wait() polls os.Stat() at an interval specified by minSleep, doubling that
// interval as long the file is not modified, upto a maximum interval specified
// by maxSleep. This allows faster detection during periods of frequent
// modification but conserves resources during periods of inactivity.
// The default values of minSleep and maxSleep can be overriden using the
// newCustomFSStatWatcher() constructor.
func newFSStatWatcher(filename string) (fsWatcher, error) {
	return newCustomFSStatWatcher(filename, defaultMinSleep, defaultMaxSleep)
}

func newCustomFSStatWatcher(filename string, minSleep, maxSleep time.Duration) (fsWatcher, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &fsStatWatcher{
		minSleep:     minSleep,
		maxSleep:     maxSleep,
		file:         file,
		lastFileSize: fileInfo.Size(),
		cancel:       make(chan struct{}),
	}, nil
}

func (w *fsStatWatcher) Wait() error {
	// After Close returns, any call to Wait must return io.EOF.
	if !w.pending.TryAdd() {
		return io.EOF
	}
	defer w.pending.Done()

	sleep := w.minSleep
	for {
		select {
		case <-w.cancel:
			// After Close returns, any call to Wait must return io.EOF.
			return io.EOF
		default:
		}
		fileInfo, err := w.file.Stat()
		if err != nil {
			return err
		} else if fileSize := fileInfo.Size(); w.lastFileSize < fileSize {
			w.lastFileSize = fileSize
			return nil
		}
		time.Sleep(sleep)
		sleep = minDuration(sleep*2, w.maxSleep)
	}
}

func (w *fsStatWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	close(w.cancel)
	w.pending.Wait()
	return w.file.Close()
}
