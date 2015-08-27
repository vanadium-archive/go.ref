// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lockfile

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"

	"v.io/x/lib/vlog"
)

const lockfileName = "agent.lock"
const tempLockfileName = "agent.templock"
const agentSocketName = "agent.sock" // Keep in sync with agentd/main.go

func CreateLockfile(dir string) error {
	f, err := ioutil.TempFile(dir, tempLockfileName)
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	cmd := makePsCommand(os.Getpid())
	cmd.Stdout = f
	cmd.Stderr = nil
	if err = cmd.Run(); err != nil {
		return err
	}
	lockfile := filepath.Join(dir, lockfileName)
	err = os.Link(f.Name(), lockfile)
	if err == nil {
		return nil
	}

	// Check for a stale lock
	lockProcessInfo, err := ioutil.ReadFile(lockfile)
	if err != nil {
		return err
	}
	if err, running := StillRunning(lockProcessInfo); running {
		return fmt.Errorf("agentd is already running:\n%s", lockProcessInfo)
	} else if err != nil {
		return err
	}

	// Delete the socket if the old agent left one behind.
	if err = os.RemoveAll(filepath.Join(dir, agentSocketName)); err != nil {
		return err
	}

	// Note(ribrdb): There's a race here between checking the file contents
	// and deleting the file, but I don't think it will be an issue in normal
	// usage.
	return os.Rename(f.Name(), lockfile)
}

var pidRegex = regexp.MustCompile("\n\\s*(\\d+)")

func StillRunning(expected []byte) (error, bool) {
	match := pidRegex.FindSubmatch(expected)
	if match == nil {
		// Corrupt lockfile. Just delete it.
		return nil, false
	}
	pid, err := strconv.Atoi(string(match[1]))
	if err != nil {
		return nil, false
	}
	// Go's os turns the standard errors into indecipherable ones,
	// so use syscall directly.
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
			return nil, false
		}
	}
	cmd := makePsCommand(pid)
	out, err := cmd.Output()
	if err != nil {
		return err, false
	}
	return nil, bytes.Equal(expected, out)
}

func RemoveLockfile(dir string) {
	path := filepath.Join(dir, lockfileName)
	if err := os.Remove(path); err != nil {
		vlog.Infof("Unable to remove lockfile %q: %v", path, err)
	}
	path = filepath.Join(dir, agentSocketName)
	if err := os.RemoveAll(path); err != nil {
		vlog.Infof("Unable to remove socket %q: %v", path, err)
	}
}

func makePsCommand(pid int) *exec.Cmd {
	return exec.Command("ps", "-o", "pid,ppid,lstart,user,comm", "-p", strconv.Itoa(pid))
}
