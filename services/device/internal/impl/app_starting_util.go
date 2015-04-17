// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

// TODO -- Ideally the code in this file would be integrated with the instance reaping,
// so we avoid having two process polling loops. This code is currently separate because
// the actions taken when the app dies (or is caught lying about its pid) prior to being
// considered running are fairly different from what's currently done by the reaper in
// handling deaths that occur after the app started successfully.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	vexec "v.io/x/ref/lib/exec"
	"v.io/x/ref/services/device/internal/suid"
)

// appWatcher watches the pid of a running app until either the pid exits or stop()
// is called
type appWatcher struct {
	pid      int           // Pid to watch
	callback func()        // Called if the pid exits or if stop() is invoked
	stopper  chan struct{} // Used to stop the appWatcher
}

func newAppWatcher(pidToWatch int, callOnPidExit func()) *appWatcher {
	return &appWatcher{
		pid:      pidToWatch,
		callback: callOnPidExit,
		stopper:  make(chan struct{}, 1),
	}
}

func (a *appWatcher) stop() {
	close(a.stopper)
}

func (a *appWatcher) watchAppPid() {
	defer a.callback()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := syscall.Kill(a.pid, 0); err != nil && err != syscall.EPERM {
				vlog.Errorf("App died in startup: pid=%d: %v", a.pid, err)
				return
			} else {
				vlog.VI(2).Infof("App pid %d is alive", a.pid)
			}

		case <-a.stopper:
			vlog.Errorf("AppWatcher was stopped")
			return
		}
	}
	// Not reached.
}

// appHandshaker is a utility to do the app handshake for a newly started app while
// reacting quickly if the app crashes. appHandshaker reads two pids from the app (one
// from the helper that forked the app, and the other from the app itself). If the app
// appears to be lying about its own pid, it will kill the app.
type appHandshaker struct {
	helperRead, helperWrite *os.File
	ctx                     *context.T
}

func (a *appHandshaker) cleanup() {
	if a.helperRead != nil {
		a.helperRead.Close()
		a.helperRead = nil
	}
	if a.helperWrite != nil {
		a.helperWrite.Close()
		a.helperWrite = nil
	}
}

// prepareToStart sets up the pipe used to talk to the helper. It must be called before
// the app is started so that the app will inherit the file descriptor
func (a *appHandshaker) prepareToStart(ctx *context.T, cmd *exec.Cmd) error {
	if suid.PipeToParentFD != (len(cmd.ExtraFiles) + vexec.FileOffset) {
		return verror.New(ErrOperationFailed, ctx,
			fmt.Sprintf("FD expected by helper (%v) was not available (%v) (%v)",
				suid.PipeToParentFD, len(cmd.ExtraFiles), vexec.FileOffset))
	}
	a.ctx = ctx

	var err error
	a.helperRead, a.helperWrite, err = os.Pipe()
	if err != nil {
		vlog.Errorf("Failed to create pipe: %v", err)
		return err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, a.helperWrite)
	return nil
}

// doAppHandshake executes the startup handshake for the app. Upon success, it returns the
// pid and appCycle manager name for the started app.
//
// handle should have been set up to use a helper for the app and handle.Start()
// and handle.Wait() should already have been called (so we know the helper is done)
func (a *appHandshaker) doHandshake(handle *vexec.ParentHandle, listener callbackListener) (int, string, error) {
	// Close our copy of helperWrite to make helperRead return EOF once the
	// helper's copy of helperWrite is closed.
	a.helperWrite.Close()
	a.helperWrite = nil

	// Get the app pid from the helper. This won't block as the helper is done
	var pid32 int32
	if err := binary.Read(a.helperRead, binary.LittleEndian, &pid32); err != nil {
		vlog.Errorf("Error reading app pid from child: %v", err)
		return 0, "", verror.New(ErrOperationFailed, a.ctx, fmt.Sprintf("failed to read pid from helper: %v", err))
	}
	pidFromHelper := int(pid32)
	vlog.VI(1).Infof("read app pid %v from child", pidFromHelper)

	// Watch the app pid in case it exits.
	pidExitedChan := make(chan struct{}, 1)
	watcher := newAppWatcher(pidFromHelper, func() {
		listener.stop()
		close(pidExitedChan)
	})
	go watcher.watchAppPid()
	defer watcher.stop()

	// Wait for the child to say it's ready and provide its own pid via the init handshake
	childReadyErrChan := make(chan error, 1)
	go func() {
		if err := handle.WaitForReady(childReadyTimeout); err != nil {
			childReadyErrChan <- verror.New(ErrOperationFailed, a.ctx, fmt.Sprintf("WaitForReady(%v) failed: %v", childReadyTimeout, err))
		}
		childReadyErrChan <- nil
	}()

	// Wait until we get the pid from the app, but return early if
	// the watcher notices that the app failed
	pidFromChild := 0

	select {
	case <-pidExitedChan:
		return 0, "", verror.New(ErrOperationFailed, a.ctx,
			fmt.Sprintf("App exited (pid %d)", pidFromHelper))

	case err := <-childReadyErrChan:
		if err != nil {
			return 0, "", err
		}
		// Note: handle.Pid() is the pid of the helper, rather than that
		// of the app that the helper then forked. ChildPid is the pid
		// received via the app startup handshake
		pidFromChild = handle.ChildPid()
	}

	if pidFromHelper != pidFromChild {
		// Something nasty is going on (the child may be lying).
		suidHelper.terminatePid(pidFromHelper, nil, nil)
		return 0, "", verror.New(ErrOperationFailed, a.ctx,
			fmt.Sprintf("Child pids do not match! (%d != %d)", pidFromHelper, pidFromChild))
	}

	// The appWatcher will stop the listener if the pid dies while waiting below
	childName, err := listener.waitForValue(childReadyTimeout)
	if err != nil {
		suidHelper.terminatePid(pidFromHelper, nil, nil)
		return 0, "", verror.New(ErrOperationFailed, a.ctx,
			fmt.Sprintf("Waiting for child name: %v", err))
	}

	return pidFromHelper, childName, nil
}
