// +build darwin freebsd linux netbsd openbsd windows

package follow

import (
	"fmt"
	"github.com/howeyc/fsnotify"
	"io"
	"sync"

	vsync "veyron/runtimes/google/lib/sync"
)

type fsNotifyWatcher struct {
	filename string
	source   *fsnotify.Watcher
	// cancel signals Wait to terminate.
	cancel chan struct{}
	// pending allows Close to block till ongoing calls to Wait terminate.
	pending vsync.WaitGroup
	// mu and closed ensure that Close is idempotent.
	mu     sync.Mutex
	closed bool // GUARDED_BY(mu)
}

// newFSNotifyWatcher returns an fsnotify-based fsWatcher.
// Wait() blocks until it receives a file modification event from fsnotify.
func newFSNotifyWatcher(filename string) (fsWatcher, error) {
	source, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := source.Watch(filename); err != nil {
		source.Close()
		return nil, err
	}
	return &fsNotifyWatcher{
		source: source,
		cancel: make(chan struct{}),
	}, nil
}

func (w *fsNotifyWatcher) Wait() error {
	// After Close returns, any call to Wait must return io.EOF.
	if !w.pending.TryAdd() {
		return io.EOF
	}
	defer w.pending.Done()

	for {
		select {
		case event := <-w.source.Event:
			if event.IsModify() {
				// Drain the event queue.
				drained := false
				for !drained {
					select {
					case <-w.source.Event:
					default:
						drained = true
					}
				}
				return nil
			}
			return fmt.Errorf("Unexpected event %v", event)
		case err := <-w.source.Error:
			return err
		case <-w.cancel:
			// After Close returns, any call to Wait must return io.EOF.
			return io.EOF
		}
	}
}

func (w *fsNotifyWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	close(w.cancel)
	w.pending.Wait()
	return w.source.Close()
}
