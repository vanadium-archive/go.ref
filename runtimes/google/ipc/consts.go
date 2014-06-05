package ipc

import "time"

const (
	// The publisher re-mounts on this period.
	publishPeriod = time.Minute

	// The client and server use this timeout for calls by default.
	defaultCallTimeout = time.Minute
)
