package sync

import (
	"sync"

	"veyron/runtimes/google/testing/concurrency"
)

// Mutex is a wrapper around the Go implementation of Mutex.
type Mutex struct {
	m sync.Mutex
}

// MUTEX INTERFACE IMPLEMENTATION

func (m *Mutex) Lock() {
	if t := concurrency.T(); t != nil {
		t.MutexLock(&m.m)
	} else {
		m.m.Lock()
	}
}

func (m *Mutex) Unlock() {
	if t := concurrency.T(); t != nil {
		t.MutexUnlock(&m.m)
	} else {
		m.m.Unlock()
	}
}
