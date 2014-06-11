package rt

import (
	"os"
	"sync"

	"veyron2"
)

type mgmtImpl struct {
	sync.RWMutex
	waiters      []chan<- string
	taskTrackers []chan<- veyron2.Task
	task         veyron2.Task
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

func (rt *vrt) TrackTask(ch chan<- veyron2.Task) {
	rt.mgmt.Lock()
	defer rt.mgmt.Unlock()
	rt.mgmt.taskTrackers = append(rt.mgmt.taskTrackers, ch)
}

func (rt *vrt) advanceTask(progress, goal int) {
	rt.mgmt.Lock()
	defer rt.mgmt.Unlock()
	rt.mgmt.task.Goal += goal
	rt.mgmt.task.Progress += progress
	for _, pl := range rt.mgmt.taskTrackers {
		select {
		case pl <- rt.mgmt.task:
		default:
		}
	}
}

func (rt *vrt) AdvanceGoal(delta int) {
	if delta <= 0 {
		return
	}
	rt.advanceTask(0, delta)
}

func (rt *vrt) AdvanceProgress(delta int) {
	if delta <= 0 {
		return
	}
	rt.advanceTask(delta, 0)
}
