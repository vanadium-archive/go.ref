package modules

import "io"

// NewRW exposes newRW for unit tests.
func NewRW() io.ReadWriteCloser {
	return newRW()
}
