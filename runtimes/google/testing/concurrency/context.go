package concurrency

import (
	"sync"
)

// context stores the abstract state of resources used in an execution
// of a concurrent program.
type context struct {
	// mutexes stores the abstract state of mutexes.
	mutexes map[*sync.Mutex]*fakeMutex
}

// newContext if the context factory.
func newContext() *context {
	return &context{
		mutexes: make(map[*sync.Mutex]*fakeMutex),
	}
}
