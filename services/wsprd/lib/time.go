package lib

import (
	"time"

	"v.io/v23/ipc"
)

const nanosecondsPerMillisecond = 1000000

// Javascript uses millisecond time units, not nanoseconds.
// This is both because they are the native time unit, and because
// otherwise JS numbers would not be capable of representing the
// full time range available to Go programs.
const JSIPCNoTimeout = ipc.NoTimeout / nanosecondsPerMillisecond

func GoToJSDuration(d time.Duration) int64 {
	return int64(d) / nanosecondsPerMillisecond
}

func JSToGoDuration(d int64) time.Duration {
	return time.Duration(d * nanosecondsPerMillisecond)
}
