package follow

import (
	"time"
)

const defaultSleep = time.Second

// newFSStatWatch returns a function that sends a nil value on the events
// channel at an interval specified by sleep.
// The event receiver must determine whether the event is spurious, or
// corresponds to a modification of the file.
func newFSTimedWatch(filename string) func(chan<- error, <-chan bool, chan<- bool) {
	return newCustomFSTimedWatch(filename, defaultSleep)
}

func newCustomFSTimedWatch(filename string, sleep time.Duration) func(chan<- error, <-chan bool, chan<- bool) {
	return func(events chan<- error, stop <-chan bool, done chan<- bool) {
		defer close(done)
		defer close(events)
		for {
			// Look for a stop command.
			select {
			case <-stop:
				return
			default:
			}
			if sendEvent(events, nil, stop) {
				return
			}
			time.Sleep(sleep)
		}
	}
}
