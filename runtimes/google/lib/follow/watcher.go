package follow

import (
	"errors"
	"sync"
)

var errWatcherClosed = errors.New("watcher has already been closed")

// fsWatcher is a tool for watching append-only modifications to a file.
// Start() spawns an event routine that asynchronously detects modifications to
// the specified file and sends corresponding events on the events channel.
// Close() terminates the event routine and stops detecting any modifications.
// However, the watcher may continue to send previously detected events.
type fsWatcher struct {
	// watch runs on the event routine, and detects modifications and sends
	// corresponding events. watch runs until it is asked to stop.
	watch func(events chan<- error, stop <-chan bool, done chan<- bool)
	// events is the channel on which events and errors are sent.
	events <-chan error
	// stop is the channel on which the event routine is told to stop.
	stop chan<- bool
	// done is the channel on which the event routine announces that it is done.
	done <-chan bool
	// mu guards closed
	mu sync.Mutex
	// closed is true iff the watcher has been closed.
	closed bool // GUARDED_BY(mu)
}

// newFSWatcher spawns an event routine that runs watch, and returns a new
// fsWatcher.
// watch is a function that detects file modifications and sends a corresponding
// nil value on the returned watcher's events channel. If an error occurs in
// watch, it is sent on the events channel. However, watch may keep running.
// To halt watch, the receiver should call Close().
//
// A sequence of modifications may correspond to one sent event. No further
// events will be sent until the original event is received. However, watch
// takes note of any modifications detected after the original event was sent,
// and sends a new event after the original event is received.
//
// The frequency at which events are generated is implementation-specific.
// Implementations may generate events even if the file has not been modified -
// the receiver should determine whether these events are spurious.
func newCustomFSWatcher(watch func(chan<- error, <-chan bool, chan<- bool)) (*fsWatcher, error) {
	events := make(chan error)
	stop := make(chan bool)
	done := make(chan bool)
	watcher := &fsWatcher{
		watch:  watch,
		events: events,
		stop:   stop,
		done:   done,
		closed: false,
	}
	go watch(events, stop, done)
	return watcher, nil
}

// Close closes the watcher synchronously.
func (w *fsWatcher) Close() error {
	// Mark the watcher closed.
	if err := w.setClosed(); err != nil {
		return err
	}

	close(w.stop)
	<-w.done
	return nil
}

func (w *fsWatcher) setClosed() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errWatcherClosed
	}
	w.closed = true
	return nil
}
