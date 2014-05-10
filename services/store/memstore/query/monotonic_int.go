package query

import (
	"sync"
)

// monotonicInt produces monotonically increasing integers.  This is useful
// for the store.QueryResult.NestedResult field.
// monotonicInt is threadsafe.
type monotonicInt struct {
	mu  sync.Mutex
	val int
}

// Next returns the next integer.  It is guaranteed to be greater than all
// previously returned integers.
func (m *monotonicInt) Next() int {
	m.mu.Lock()
	ret := m.val
	m.val++
	m.mu.Unlock()
	return ret
}
