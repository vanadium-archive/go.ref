package follow

import (
	"time"
)

const defaultSleep = time.Second

// newFSStatWatch returns a function that sends a nil value on the events
// channel at an interval specified by sleep.
// The event receiver must determine whether the event is spurious, or
// corresponds to a modification of the file.
func newFSTimedWatch(filename string) func(chan<- error, chan<- struct{}, <-chan struct{}, chan<- struct{}) {
	return newCustomFSTimedWatch(filename, defaultSleep)
}

func newCustomFSTimedWatch(filename string, sleep time.Duration) func(chan<- error, chan<- struct{}, <-chan struct{}, chan<- struct{}) {
	return func(events chan<- error, initialized chan<- struct{}, stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		defer close(events)

		close(initialized)

		for {
			// Look for a stop command.
			select {
			case <-stop:
				return
			default:
			}
			if !sendEvent(events, nil, stop) {
				return
			}
			time.Sleep(sleep)
		}
	}
}
