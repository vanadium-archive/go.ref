package impl

import (
	"syscall"
	"time"

	"v.io/core/veyron2/vlog"
)

type pidInstanceDirPair struct {
	instanceDir string
	pid         int
}

type reaper chan pidInstanceDirPair

// TODO(rjkroege): extend to permit initializing the set of watched
// pids from an inspection of the file system.
func newReaper() reaper {
	c := make(reaper)
	go processStatusPolling(c, make(map[string]int))
	return c
}

func suspendTask(idir string) {
	if err := transitionInstance(idir, started, suspended); err != nil {
		// This may fail under two circumstances.
		// 1. The app has crashed between where startCmd invokes
		// startWatching and where the invoker sets the state to started.
		// 2. Remove experiences a failure (e.g. filesystem becoming R/O)
		vlog.Errorf("reaper transitionInstance(%v, %v, %v) failed: %v\n", idir, started, suspended, err)
	}
}

// processStatusPolling polls for the continued existence of a set of tracked
// pids.
// TODO(rjkroege): There are nicer ways to provide this functionality.
// For example, use the kevent facility in darwin or replace init.
// See http://www.incenp.org/dvlpt/wait4.html for inspiration.
func processStatusPolling(cq reaper, trackedPids map[string]int) {
	poll := func() {
		for idir, pid := range trackedPids {
			switch err := syscall.Kill(pid, 0); err {
			case syscall.ESRCH:
				// No such PID.
				vlog.VI(2).Infof("processStatusPolling discovered pid %d ended", pid)
				suspendTask(idir)
				delete(trackedPids, idir)
			case nil, syscall.EPERM:
				vlog.VI(2).Infof("processStatusPolling saw live pid: %d", pid)
				// The task exists and is running under the same uid as
				// the device manager or the task exists and is running
				// under a different uid as would be the case if invoked
				// via suidhelper. In this case do, nothing.

				// This implementation cannot detect if a process exited
				// and was replaced by an arbitrary non-Vanadium process
				// within the polling interval.
				// TODO(rjkroege): Consider probing the appcycle service of
				// the pid to confirm.
			default:
				// The kill system call manpage says that this can only happen
				// if the kernel claims that 0 is an invalid signal.
				// Only a deeply confused kernel would say this so just give
				// up.
				vlog.Panicf("processStatusPolling: unanticpated result from sys.Kill: %v", err)
			}
		}
	}

	for {
		select {
		case p, ok := <-cq:
			if !ok {
				return
			}
			if p.pid < 0 {
				delete(trackedPids, p.instanceDir)
			} else {
				trackedPids[p.instanceDir] = p.pid
				poll()
			}
		case <-time.After(time.Second):
			// Poll once / second.
			poll()
		}
	}
}

// startWatching begins watching process pid's state. This routine
// assumes that pid already exists. Since pid is delivered to the device
// manager by RPC callback, this seems reasonable.
func (r reaper) startWatching(idir string, pid int) {
	r <- pidInstanceDirPair{instanceDir: idir, pid: pid}
}

// stopWatching stops watching process pid's state.
func (r reaper) stopWatching(idir string) {
	r <- pidInstanceDirPair{instanceDir: idir, pid: -1}
}

func (r reaper) shutdown() {
	close(r)
}
