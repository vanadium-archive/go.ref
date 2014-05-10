package mgmt

import (
	"os"
	"sync"

	"veyron2/mgmt"
)

type impl struct {
	sync.RWMutex
	waiters []chan<- string
}

// New returns a new instance of the mgmt runtime implementation.
func New() mgmt.Runtime {
	return new(impl)
}

// Stop implements mgmt.Runtime.
func (m *impl) Stop() {
	m.RLock()
	defer m.RUnlock()
	if len(m.waiters) == 0 {
		os.Exit(mgmt.UnhandledStopExitCode)
	}
	for _, w := range m.waiters {
		select {
		case w <- mgmt.LocalStop:
		default:
		}
	}
}

// ForceStop implements mgmt.Runtime.
func (*impl) ForceStop() {
	os.Exit(mgmt.ForceStopExitCode)
}

// WaitForStop implements mgmt.Runtime.
func (m *impl) WaitForStop(ch chan<- string) {
	m.Lock()
	defer m.Unlock()
	m.waiters = append(m.waiters, ch)
}
