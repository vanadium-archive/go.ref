package concurrency

// transitionKind identifies the kind of transition.
type transitionKind int

const (
	tNil transitionKind = iota
	tGoParent
	tGoChild
	tGoExit
	tMutexLock
	tMutexUnlock
)

// transition records information about the abstract program
// transition of a thread.
type transition struct {
	// tid identifies the thread this transition belongs to.
	tid TID
	// clock records the logical time at the beginning of this
	// transition as perceived by the thread this transition belongs to.
	clock map[TID]int
	// kind records the kind of this transition.
	kind transitionKind
	// enable identifies whether this transition is enabled.
	enabled bool
	// readSet identifies the set of abstract resources read by this
	// transition.
	readSet resourceSet
	// writeSet identifies the set of abstract resources written by this
	// transition.
	writeSet resourceSet
}
