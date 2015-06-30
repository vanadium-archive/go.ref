// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"os"
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
)

const (
	AppcycleReconciliation = "V23_APPCYCLE_RECONCILIATION"
)

var (
	errPIDIsNotInteger = verror.Register(pkgPath+".errPIDIsNotInteger", verror.NoRetry, "{1:}{2:} __debug/stats/system/pid isn't an integer{:_}")

	v23PIDMgmt = true
)

func init() {
	// TODO(rjkroege): Environment variables do not survive device manager updates.
	// Use an alternative mechanism.
	if os.Getenv(AppcycleReconciliation) != "" {
		v23PIDMgmt = false
	}
}

type pidInstanceDirPair struct {
	instanceDir string
	pid         int
}

type reaper struct {
	c          chan pidInstanceDirPair
	startState *appRunner
	ctx        *context.T
}

var stashedPidMap map[string]int

func newReaper(ctx *context.T, root string, startState *appRunner) (*reaper, error) {
	pidMap, restartCandidates, err := findAllTheInstances(ctx, root)

	// Used only by the testing code that verifies that all processes
	// have been shutdown.
	stashedPidMap = pidMap
	if err != nil {
		return nil, err
	}

	r := &reaper{
		c:          make(chan pidInstanceDirPair),
		startState: startState,
		ctx:        ctx,
	}
	r.startState.reap = r
	go r.processStatusPolling(ctx, pidMap)

	// Restart daemon jobs if they're not running (say because the machine crashed.)
	for _, idir := range restartCandidates {
		go r.startState.restartAppIfNecessary(ctx, idir)
	}
	return r, nil
}

func markNotRunning(ctx *context.T, idir string) {
	if err := transitionInstance(idir, device.InstanceStateRunning, device.InstanceStateNotRunning); err != nil {
		// This may fail under two circumstances.
		// 1. The app has crashed between where startCmd invokes
		// startWatching and where the invoker sets the state to running.
		// 2. Remove experiences a failure (e.g. filesystem becoming R/O)
		// 3. The app is in the process of being Kill'ed when the reaper poll
		// finds the process dead and attempts a restart.
		ctx.Errorf("reaper transitionInstance(%v, %v, %v) failed: %v\n", idir, device.InstanceStateRunning, device.InstanceStateNotRunning, err)
	}
}

// processStatusPolling polls for the continued existence of a set of
// tracked pids. TODO(rjkroege): There are nicer ways to provide this
// functionality. For example, use the kevent facility in darwin or
// replace init. See http://www.incenp.org/dvlpt/wait4.html for
// inspiration.
func (r *reaper) processStatusPolling(ctx *context.T, trackedPids map[string]int) {
	poll := func(ctx *context.T) {
		for idir, pid := range trackedPids {
			switch err := syscall.Kill(pid, 0); err {
			case syscall.ESRCH:
				// No such PID.
				go r.startState.restartAppIfNecessary(ctx, idir)
				ctx.VI(2).Infof("processStatusPolling discovered pid %d ended", pid)
				markNotRunning(ctx, idir)
				go r.startState.restartAppIfNecessary(ctx, idir)
				delete(trackedPids, idir)
			case nil, syscall.EPERM:
				ctx.VI(2).Infof("processStatusPolling saw live pid: %d", pid)
				// The task exists and is running under the same uid as
				// the device manager or the task exists and is running
				// under a different uid as would be the case if invoked
				// via suidhelper. In this case do, nothing.

				// This implementation cannot detect if a process exited
				// and was replaced by an arbitrary non-Vanadium process
				// within the polling interval.
				// TODO(rjkroege): Probe the appcycle service of the app
				// to confirm that its pid is valid iff v23PIDMgmt
				// is false.

				// TODO(rjkroege): if we can't connect to the app here via
				// the appcycle manager, the app was probably started under
				// a different agent and cannot be managed. Perhaps we should
				// then kill the app and restart it?
			default:
				// The kill system call manpage says that this can only happen
				// if the kernel claims that 0 is an invalid signal.
				// Only a deeply confused kernel would say this so just give
				// up.
				ctx.Panicf("processStatusPolling: unanticpated result from sys.Kill: %v", err)
			}
		}
	}

	for {
		select {
		case p, ok := <-r.c:
			switch {
			case !ok:
				return
			case p.pid == -1: // stop watching this instance
				delete(trackedPids, p.instanceDir)
				poll(ctx)
			case p.pid == -2: // kill the process
				if pid, ok := trackedPids[p.instanceDir]; ok {
					if err := suidHelper.terminatePid(ctx, pid, nil, nil); err != nil {
						ctx.Errorf("Failure to kill: %v", err)
					}
				}
			case p.pid < 0:
				ctx.Panicf("invalid pid %v", p.pid)
			default:
				trackedPids[p.instanceDir] = p.pid
				poll(ctx)
			}
		case <-time.After(time.Second):
			// Poll once / second.
			poll(ctx)
		}
	}
}

// startWatching begins watching process pid's state. This routine
// assumes that pid already exists. Since pid is delivered to the device
// manager by RPC callback, this seems reasonable.
func (r *reaper) startWatching(idir string, pid int) {
	r.c <- pidInstanceDirPair{instanceDir: idir, pid: pid}
}

// stopWatching stops watching process pid's state.
func (r *reaper) stopWatching(idir string) {
	r.c <- pidInstanceDirPair{instanceDir: idir, pid: -1}
}

// forciblySuspend terminates the process pid
func (r *reaper) forciblySuspend(idir string) {
	r.c <- pidInstanceDirPair{instanceDir: idir, pid: -2}
}

func (r *reaper) shutdown() {
	close(r.c)
}

type pidErrorTuple struct {
	ipath        string
	pid          int
	err          error
	mightRestart bool
}

// In seconds.
const appCycleTimeout = 5

// processStatusViaAppCycleMgr updates the status based on getting the
// pid from the AppCycleMgr because the data in the instance info might
// be outdated: the app may have exited and an arbitrary non-Vanadium
// process may have been executed with the same pid.
func processStatusViaAppCycleMgr(ctx *context.T, c chan<- pidErrorTuple, instancePath string, info *instanceInfo, state device.InstanceState) {
	nctx, _ := context.WithTimeout(ctx, appCycleTimeout*time.Second)

	name := naming.Join(info.AppCycleMgrName, "__debug/stats/system/pid")
	sclient := stats.StatsClient(name)
	v, err := sclient.Value(nctx)
	if err != nil {
		ctx.Infof("Instance: %v error: %v", instancePath, err)
		// No process is actually running for this instance.
		ctx.VI(2).Infof("perinstance stats fetching failed: %v", err)
		if err := transitionInstance(instancePath, state, device.InstanceStateNotRunning); err != nil {
			ctx.Errorf("transitionInstance(%s,%s,%s) failed: %v", instancePath, state, device.InstanceStateNotRunning, err)
		}
		// We only want to restart apps that were Running or Launching.
		if state == device.InstanceStateLaunching || state == device.InstanceStateRunning {
			c <- pidErrorTuple{ipath: instancePath, err: err, mightRestart: true}
		} else {
			c <- pidErrorTuple{ipath: instancePath, err: err}
		}
		return
	}
	// Convert the stat value from *vdl.Value into an int pid.
	var pid int
	if err := vdl.Convert(&pid, v); err != nil {
		err = verror.New(errPIDIsNotInteger, ctx, err)
		ctx.Errorf(err.Error())
		c <- pidErrorTuple{ipath: instancePath, err: err}
		return
	}

	ptuple := pidErrorTuple{ipath: instancePath, pid: pid}

	// Update the instance info.
	if info.Pid != pid {
		info.Pid = pid
		ptuple.err = saveInstanceInfo(ctx, instancePath, info)
	}

	// The instance was found to be running, so update its state accordingly
	// (in case the device restarted while the instance was in one of the
	// transitional states like launching, dying, etc).
	if err := transitionInstance(instancePath, state, device.InstanceStateRunning); err != nil {
		ctx.Errorf("transitionInstance(%s,%v,%s) failed: %v", instancePath, state, device.InstanceStateRunning, err)
	}

	ctx.VI(0).Infof("perInstance go routine for %v ending", instancePath)
	c <- ptuple
}

// processStatusViaKill updates the status based on sending a kill signal
// to the process. This assumes that most processes on the system are
// likely to be managed by the device manager and a live process is not
// responsive because the agent has been restarted rather than being
// created through a different means.
func processStatusViaKill(ctx *context.T, c chan<- pidErrorTuple, instancePath string, info *instanceInfo, state device.InstanceState) {
	pid := info.Pid

	switch err := syscall.Kill(pid, 0); err {
	case syscall.ESRCH:
		// No such PID.
		if err := transitionInstance(instancePath, state, device.InstanceStateNotRunning); err != nil {
			ctx.Errorf("transitionInstance(%s,%s,%s) failed: %v", instancePath, state, device.InstanceStateNotRunning, err)
		}
		// We only want to restart apps that were Running or Launching.
		if state == device.InstanceStateLaunching || state == device.InstanceStateRunning {
			c <- pidErrorTuple{ipath: instancePath, err: err, pid: pid, mightRestart: true}
		} else {
			c <- pidErrorTuple{ipath: instancePath, err: err, pid: pid}
		}
	case nil, syscall.EPERM:
		// The instance was found to be running, so update its state.
		if err := transitionInstance(instancePath, state, device.InstanceStateRunning); err != nil {
			ctx.Errorf("transitionInstance(%s,%v, %v) failed: %v", instancePath, state, device.InstanceStateRunning, err)
		}
		ctx.VI(0).Infof("perInstance go routine for %v ending", instancePath)
		c <- pidErrorTuple{ipath: instancePath, pid: pid}
	}
}

func perInstance(ctx *context.T, instancePath string, c chan<- pidErrorTuple, wg *sync.WaitGroup) {
	defer wg.Done()
	ctx.Infof("Instance: %v", instancePath)
	state, err := getInstanceState(instancePath)
	switch state {
	// Ignore apps already in deleted and not running states.
	case device.InstanceStateNotRunning:
		c <- pidErrorTuple{ipath: instancePath}
		return
	case device.InstanceStateDeleted:
		return
	// If the app was updating, it means it was already not running, so just
	// update its state back to not running.
	case device.InstanceStateUpdating:
		if err := transitionInstance(instancePath, state, device.InstanceStateNotRunning); err != nil {
			ctx.Errorf("transitionInstance(%s,%s,%s) failed: %v", instancePath, state, device.InstanceStateNotRunning, err)
		}
		return
	}
	ctx.VI(2).Infof("perInstance firing up on %s", instancePath)

	// Read the instance data.
	info, err := loadInstanceInfo(ctx, instancePath)
	if err != nil {
		ctx.Errorf("loadInstanceInfo failed: %v", err)
		// Something has gone badly wrong.
		// TODO(rjkroege,caprita): Consider removing the instance or at
		// least set its state to something indicating error?
		c <- pidErrorTuple{err: err, ipath: instancePath}
		return
	}

	// Remaining states: Launching, Running, Dying. Of these,
	// daemon mode will restart Launching and Running if the process
	// is not alive.
	if !v23PIDMgmt {
		processStatusViaAppCycleMgr(ctx, c, instancePath, info, state)
		return
	}
	processStatusViaKill(ctx, c, instancePath, info, state)
}

// Digs through the directory hierarchy
func findAllTheInstances(ctx *context.T, root string) (map[string]int, []string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "app*", "installation*", "instances", "instance*"))
	if err != nil {
		return nil, nil, err
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

	restartCandidates := make([]string, len(paths))
	for p := range pidchan {
		if p.err != nil {
			ctx.Errorf("instance at %s had an error: %v", p.ipath, p.err)
		}
		if p.pid > 0 {
			pidmap[p.ipath] = p.pid
		}
		if p.mightRestart {
			restartCandidates = append(restartCandidates, p.ipath)
		}
	}
	return pidmap, restartCandidates, nil
}

// RunningChildrenProcesses uses the reaper to verify that a test has
// successfully shut down all processes.
func RunningChildrenProcesses() bool {
	return len(stashedPidMap) > 0
}
