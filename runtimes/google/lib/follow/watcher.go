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
// A sequence of modifications may correspond to one sent event. Watch guarantees
// that at least one event is received after the most recent modification.
//
// The frequency at which events are generated is implementation-specific.
// Implementations may generate events even if the file has not been modified -
// the receiver should determine whether these events are spurious.
func newCustomFSWatcher(watch func(chan<- error, <-chan bool, chan<- bool)) (*fsWatcher, error) {
	events := make(chan error, 1)
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

// sendEvent sends the event on the events channel. sendEvent expects the buffer
// size of the events channel to be exactly one.
// If the event is not nil, it will always be sent, and sendEvent blocks on the
// events channel.
// Otherwise, if there is already an event in the channel, sendEvent won't send
// the event. This coalesces events that happen faster than the receiver can
// process them.
// sendEvent can be preempted by a stop request, and returns true iff the event
// was sent or coalesced with an existing event.
func sendEvent(events chan<- error, event error, stop <-chan bool) bool {
	if event == nil {
		select {
		case <-stop:
			return false
		case events <- nil:
		default:
		}
		return true
	}
	select {
	case <-stop:
		return false
	case events <- event:
		return true
	}
}
