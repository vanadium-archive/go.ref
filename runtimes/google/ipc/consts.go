package ipc

import "time"

const (
	// The publisher re-mounts on this period.
	publishPeriod = time.Minute
	// The publisher adds this much slack to each TTL.
	mountTTLSlack = 20 * time.Second

	// The client and server use this timeout for calls by default.
	defaultCallTimeout = time.Minute
)
