package vc

import "time"

// cancelChannel creates a channel usable by bqueue.Writer.Put and upcqueue.Get
// to cancel the calls if they have not completed by the provided deadline.
// It returns two channels, the first is the channel to supply to Put or Get
// which will be closed when the deadline expires.
// The second is the quit channel, which can be closed to clean up resources
// when the first is no longer needed (the deadline is no longer worth enforcing).
//
// A zero deadline (time.Time.IsZero) implies that no cancellation is desired.
func cancelChannel(deadline time.Time) (expired, quit chan struct{}) {
	if deadline.IsZero() {
		return nil, nil
	}
	expired = make(chan struct{})
	quit = make(chan struct{})
	timer := time.NewTimer(deadline.Sub(time.Now()))

	go func() {
		select {
		case <-timer.C:
		case <-quit:
		}
		timer.Stop()
		close(expired)
	}()
	return expired, quit
}

// timeoutError implements net.Error with Timeout returning true.
type timeoutError struct{}

func (t timeoutError) Error() string   { return "deadline exceeded" }
func (t timeoutError) Timeout() bool   { return true }
func (t timeoutError) Temporary() bool { return false }
