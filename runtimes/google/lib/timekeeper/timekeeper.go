package timekeeper

import "time"

// TimeKeeper is meant as a drop-in replacement for using the time package
// directly, and allows testing code to substitute a suitable implementation.
// The meaning of duration and current time depends on the implementation (may
// be a simulated time).
type TimeKeeper interface {
	// After waits for the duration to elapse and then sends the current
	// time on the returned channel.
	After(d time.Duration) <-chan time.Time
	// Sleep pauses the current goroutine for at least the duration d. A
	// negative or zero duration causes Sleep to return immediately.
	Sleep(d time.Duration)
}

// realTime is the default implementation of TimeKeeper, using the time package.
type realTime struct{}

var rt realTime

// After implements TimeKeeper.After.
func (t *realTime) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// Sleep implements TimeKeeper.Sleep.
func (t *realTime) Sleep(d time.Duration) {
	time.Sleep(d)
}

// RealTime returns a default instance of TimeKeeper.
func RealTime() TimeKeeper {
	return &rt
}
