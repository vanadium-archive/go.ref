package follow

import (
	"os"
	"time"
)

const (
	defaultMinSleep = time.Second
	defaultMaxSleep = time.Minute
)

// newFSStatWatch returns a function that polls os.Stat(), observing file size.
// If the file size is larger than the previously-recorded file size,
// fsStatWatcher assumes the file has been modified and sends a nil value on
// the events channel.
// The function starts polling os.Stat() at an interval specified by
// minSleep, doubling that interval as long the file is not modified, upto
// a maximum interval specified by maxSleep. The interval is reset to minSleep
// once the file is modified. This allows faster detection during periods of
// frequent modification but conserves resources during periods of inactivity.
// The default values of minSleep and maxSleep can be overriden using the
// newCustomFSStatWatcher() constructor.
func newFSStatWatch(filename string) func(chan<- error, chan<- struct{}, <-chan struct{}, chan<- struct{}) {
	return newCustomFSStatWatch(filename, defaultMinSleep, defaultMaxSleep)
}

func newCustomFSStatWatch(filename string, minSleep, maxSleep time.Duration) func(chan<- error, chan<- struct{}, <-chan struct{}, chan<- struct{}) {
	return func(events chan<- error, initialized chan<- struct{}, stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		defer close(events)
		file, err := os.Open(filename)
		if err != nil {
			events <- err
			return
		}
		defer file.Close()
		fileInfo, err := file.Stat()
		if err != nil {
			events <- err
			return
		}
		initialFileSize := fileInfo.Size()

		close(initialized)

		lastFileSize := initialFileSize
		sleep := minSleep
		for {
			// Look for a stop command.
			select {
			case <-stop:
				return
			default:
			}
			fileInfo, err := file.Stat()
			if err != nil {
				if !sendEvent(events, err, stop) {
					return
				}
			} else if fileSize := fileInfo.Size(); lastFileSize < fileSize {
				lastFileSize = fileSize
				if !sendEvent(events, nil, stop) {
					return
				}
				sleep = minSleep
			}
			time.Sleep(sleep)
			sleep = minDuration(sleep*2, maxSleep)
		}
	}
}
