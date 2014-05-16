package rt

import (
	"os"
	"sync"

	"veyron2"
)

type mgmtImpl struct {
	sync.RWMutex
	waiters []chan<- string
}

func (rt *vrt) Stop() {
	rt.mgmt.RLock()
	defer rt.mgmt.RUnlock()
	if len(rt.mgmt.waiters) == 0 {
		os.Exit(veyron2.UnhandledStopExitCode)
	}
	for _, w := range rt.mgmt.waiters {
		select {
		case w <- veyron2.LocalStop:
		default:
		}
	}
}

func (*vrt) ForceStop() {
	os.Exit(veyron2.ForceStopExitCode)
}

func (rt *vrt) WaitForStop(ch chan<- string) {
	rt.mgmt.Lock()
	defer rt.mgmt.Unlock()
	rt.mgmt.waiters = append(rt.mgmt.waiters, ch)
}
