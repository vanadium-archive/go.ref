// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/device"
	"v.io/v23/services/stats"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

var (
	errPIDIsNotInteger = verror.Register(pkgPath+".errPIDIsNotInteger", verror.NoRetry, "{1:}{2:} __debug/stats/system/pid isn't an integer{:_}")
)

type pidInstanceDirPair struct {
	instanceDir string
	pid         int
}

type reaper chan pidInstanceDirPair

func newReaper(ctx *context.T, root string) (reaper, error) {
	pidMap, err := findAllTheInstances(ctx, root)
	if err != nil {
		return nil, err
	}

	c := make(reaper)
	go processStatusPolling(c, pidMap)
	return c, nil
}

func suspendTask(idir string) {
	if err := transitionInstance(idir, device.InstanceStateStarted, device.InstanceStateSuspended); err != nil {
		// This may fail under two circumstances.
		// 1. The app has crashed between where startCmd invokes
		// startWatching and where the invoker sets the state to started.
		// 2. Remove experiences a failure (e.g. filesystem becoming R/O)
		vlog.Errorf("reaper transitionInstance(%v, %v, %v) failed: %v\n", idir, device.InstanceStateStarted, device.InstanceStateSuspended, err)
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

type pidErrorTuple struct {
	ipath string
	pid   int
	err   error
}

// In seconds.
const appCycleTimeout = 5

func perInstance(ctx *context.T, instancePath string, c chan<- pidErrorTuple, wg *sync.WaitGroup) {
	defer wg.Done()
	vlog.Infof("Instance: %v", instancePath)
	state, err := getInstanceState(instancePath)
	switch state {
	// Ignore apps already in the suspended and stopped states.
	case device.InstanceStateStopped:
		return
	case device.InstanceStateSuspended:
		return
	// If the app was updating, it means it was already suspended, so just
	// update its state back to suspended.
	case device.InstanceStateUpdating:
		if err := transitionInstance(instancePath, state, device.InstanceStateSuspended); err != nil {
			vlog.Errorf("transitionInstance(%s,%s,%s) failed: %v", instancePath, state, device.InstanceStateSuspended, err)
		}
		return
	}
	vlog.VI(2).Infof("perInstance firing up on %s", instancePath)
	nctx, _ := context.WithTimeout(ctx, appCycleTimeout*time.Second)

	var ptuple pidErrorTuple
	ptuple.ipath = instancePath

	// Read the instance data.
	info, err := loadInstanceInfo(ctx, instancePath)
	if err != nil {
		vlog.Errorf("loadInstanceInfo failed: %v", err)

		// Something has gone badly wrong.
		// TODO(rjkroege,caprita): Consider removing the instance or at
		// least set its state to something indicating error?
		ptuple.err = err
		c <- ptuple
		return
	}

	// Get the  pid from the AppCycleMgr because the data in the instance
	// info might be outdated: the app may have exited and an arbitrary
	// non-Vanadium process may have been executed with the same pid.
	name := naming.Join(info.AppCycleMgrName, "__debug/stats/system/pid")
	sclient := stats.StatsClient(name)
	v, err := sclient.Value(nctx)
	if err != nil {
		vlog.Infof("Instance: %v error: %v", instancePath, err)
		// No process is actually running for this instance.
		vlog.VI(2).Infof("perinstance stats fetching failed: %v", err)
		if err := transitionInstance(instancePath, state, device.InstanceStateSuspended); err != nil {
			vlog.Errorf("transitionInstance(%s,%s,%s) failed: %v", instancePath, state, device.InstanceStateSuspended, err)
		}
		ptuple.err = err
		c <- ptuple
		return
	}
	// Convert the stat value from *vdl.Value into an int pid.
	var pid int
	if err := vdl.Convert(&pid, v); err != nil {
		ptuple.err = verror.New(errPIDIsNotInteger, ctx, err)
		vlog.Errorf(ptuple.err.Error())
		c <- ptuple
		return
	}

	ptuple.pid = pid
	// Update the instance info.
	if info.Pid != pid {
		info.Pid = pid
		ptuple.err = saveInstanceInfo(ctx, instancePath, info)
	}
	// The instance was found to be running, so update its state accordingly
	// (in case the device restarted while the instance was in one of the
	// transitional states like starting, suspending, etc).
	if err := transitionInstance(instancePath, state, device.InstanceStateStarted); err != nil {
		vlog.Errorf("transitionInstance(%s,%s) failed: %v", instancePath, state, device.InstanceStateStarted, err)
	}

	vlog.VI(0).Infof("perInstance go routine for %v ending", instancePath)
	c <- ptuple
}

// Digs through the directory hierarchy
func findAllTheInstances(ctx *context.T, root string) (map[string]int, error) {
	paths, err := filepath.Glob(filepath.Join(root, "app*", "installation*", "instances", "instance*"))
	if err != nil {
		return nil, err
	}

	pidmap := make(map[string]int)
	pidchan := make(chan pidErrorTuple, len(paths))
	var wg sync.WaitGroup

	for _, pth := range paths {
		wg.Add(1)
		go perInstance(ctx, pth, pidchan, &wg)
	}
	wg.Wait()
	close(pidchan)

	for p := range pidchan {
		if p.err != nil {
			vlog.Errorf("instance at %s had an error: %v", p.ipath, p.err)
		}
		if p.pid > 0 {
			pidmap[p.ipath] = p.pid
		}
	}
	return pidmap, nil
}
