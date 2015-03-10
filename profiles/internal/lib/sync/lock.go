package sync

import "sync"

// DebugMutex supports checking whether a mutex is locked.
type DebugMutex struct {
	mutex    sync.Mutex
	isLocked bool
}

func (m *DebugMutex) Lock() {
	m.mutex.Lock()
	m.isLocked = true
}

func (m *DebugMutex) Unlock() {
	m.CheckLocked()
	m.isLocked = false
	m.mutex.Unlock()
}

// CheckLocked panics if the lock is not held.
func (m *DebugMutex) CheckLocked() {
	if !m.isLocked {
		panic("Mutex is not locked")
	}
}
