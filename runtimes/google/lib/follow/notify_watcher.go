// +build darwin freebsd linux netbsd openbsd windows

package follow

import (
	"github.com/howeyc/fsnotify"
)

// newFSNotifyWatch returns a function that listens on fsnotify and sends
// corresponding modification events.
// For each fsnotify modification event, a single nil value is sent on the
// events channel. Further fsnotify events are not received until the nil
// value is received from the events channel.
// The function sends any errors on the events channel.
func newFSNotifyWatch(filename string) func(chan<- error, <-chan bool, chan<- bool) {
	return func(events chan<- error, stop <-chan bool, done chan<- bool) {
		defer close(done)
		defer close(events)
		source, err := fsnotify.NewWatcher()
		if err != nil {
			events <- err
			return
		}
		defer source.Close()
		if err := source.Watch(filename); err != nil {
			events <- err
			return
		}
		for {
			// Receive:
			//  (A) An fsnotify modification event. Send nil.
			//  (B) An fsnotify error. Send the error.
			//  (C) A stop command. Stop listening and clean up.
			select {
			case event := <-source.Event:
				if event.IsModify() {
					if sendEvent(events, nil, stop) {
						return
					}
				}
			case err := <-source.Error:
				if sendEvent(events, err, stop) {
					return
				}
			case <-stop:
				return
			}
			// As select does not prioritize cases, it could starve
			// the stop case. Look for a stop command again.
			select {
			case <-stop:
				return
			default:
			}
		}
	}
}
