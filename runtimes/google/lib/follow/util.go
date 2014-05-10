package follow

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

// composeErrors composes several (some nil) errors, returning a single error:
//  (A) If the input errors are all nil, nil is returned.
//  (B) If exactly one error is non-nil, that error is returned.
//  (C) Otherwise, a composite error is returned. The composite error message
//      lists all non-nil individual error messages. However, the composite
//      error does not provide direct access to individual errors.
func composeErrors(errs ...error) error {
	// n is the number of non-nil errors.
	n := 0
	// lastErr is the last-seen non-nil error.
	var lastErr error
	for _, err := range errs {
		if err != nil {
			n++
			lastErr = err
		}
	}
	// all input errors are nil, return nil.
	if n == 0 {
		return nil
	}
	// exactly one error is non-nil, return it.
	if n == 1 {
		return lastErr
	}
	// more than one error is non-nil, build a composite error message.
	var msgBuffer bytes.Buffer
	fmt.Fprintf(&msgBuffer, "Encountered %v errors:\n", n)
	i := 0
	for _, err := range errs {
		if err != nil {
			i++
			fmt.Fprintf(&msgBuffer, "error %d: %v:\n", i, err)
		}
	}
	return errors.New(msgBuffer.String())
}

// minDuration takes two durations and returns the shorter.
func minDuration(d1, d2 time.Duration) time.Duration {
	if d2 < d1 {
		return d2
	}
	return d1
}

// sendEvent sends the err on the events channel, but can be preempted by
// a stop request. sendEvent returns true iff a stop was requested.
func sendEvent(events chan<- error, err error, stop <-chan bool) bool {
	select {
	case events <- err:
		return false
	case <-stop:
		return true
	}
}
