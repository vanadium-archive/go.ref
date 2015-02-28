package concurrency

// choice enumerates the program transitions to choose from and
// identifies which transition is to be taken next.
type choice struct {
	// next records the thread identifier for the thread that was
	// selected to be scheduled next.
	next TID
	// transitions records the transitions for all the threads that
	// could have been scheduled next.
	transitions map[TID]*transition
}

// newChoice is the choice factory.
func newChoice() *choice {
	return &choice{
		transitions: make(map[TID]*transition),
	}
}
