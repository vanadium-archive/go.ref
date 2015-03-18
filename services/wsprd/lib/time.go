package lib

import "time"

// Javascript uses millisecond time units.  This is both because they are the
// native time unit, and because otherwise JS numbers would not be capable of
// representing the full time range available to Go programs.
//
// TODO(bjornick,toddw): Pick a better sentry for no timeout, or change to using
// VDL time.WireDeadline.
const JSRPCNoTimeout = int64(6307200000000) // 200 years in milliseconds

func GoToJSDuration(d time.Duration) int64 {
	return int64(d / time.Millisecond)
}

func JSToGoDuration(d int64) time.Duration {
	return time.Duration(d) * time.Millisecond
}
