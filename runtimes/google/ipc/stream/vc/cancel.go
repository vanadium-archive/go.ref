package vc

import "time"

// cancelChannel creates a channel usable by bqueue.Writer.Put and upcqueue.Get
// to cancel the calls if they have not completed by the provided deadline.
//
// A zero deadline (time.Time.IsZero) implies that no cancellation is desired.
func cancelChannel(deadline time.Time) chan struct{} {
	if deadline.IsZero() {
		return nil
	}
	sc := make(chan struct{})
	tc := time.After(deadline.Sub(time.Now()))
	go func(dst chan struct{}, src <-chan time.Time) {
		<-src
		close(dst)
	}(sc, tc)
	return sc
}

// timeoutError implements net.Error with Timeout returning true.
type timeoutError struct{}

func (t timeoutError) Error() string   { return "deadline exceeded" }
func (t timeoutError) Timeout() bool   { return true }
func (t timeoutError) Temporary() bool { return false }
