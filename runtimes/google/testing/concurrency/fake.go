package concurrency

// mutexState is a type to represent different states of a mutex.
type mutexState uint64

// enumeration of different mutex states.
const (
	mutexFree mutexState = iota
	mutexLocked
)

// fakeMutex is an abstract representation of a mutex.
type fakeMutex struct {
	// clock records the logical time of the last access to the mutex.
	clock clock
	// state records the state of the mutex.
	state mutexState
}

// newFakeMutex is the fakeMutex factory.
func newFakeMutex(clock clock) *fakeMutex {
	return &fakeMutex{clock: clock.clone()}
}

// free checks if the mutex is available.
func (fm *fakeMutex) free() bool {
	return fm.state == mutexFree
}

// lock models the action of locking the mutex.
func (fm *fakeMutex) lock() {
	if fm.state != mutexFree {
		panic("Locking a mutex that is already locked.")
	}
	fm.state = mutexLocked
}

// unlock models the action of unlocking the mutex.
func (fm *fakeMutex) unlock() {
	if fm.state != mutexLocked {
		panic("Unlocking a mutex that is not locked.")
	}
	fm.state = mutexFree
}
