package ipc

import "time"

const (
	// The publisher re-mounts on this period.
	publishPeriod = time.Minute

	// The server uses this timeout for incoming calls before the real timeout is known.
	defaultCallTimeout = time.Minute
)
