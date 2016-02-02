// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lockutil contains utilities for building file locks.
package lockutil

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
)

func makePsCommand(pid int) *exec.Cmd {
	return exec.Command("ps", "-o", "pid,ppid,lstart,user,comm", "-p", strconv.Itoa(pid))
}

// CreatePIDFile writes information about the current process (like its PID) to
// the specified file in the specified directory.
func CreatePIDFile(dir, file string) (string, error) {
	f, err := ioutil.TempFile(dir, file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmd := makePsCommand(os.Getpid())
	cmd.Stdout = f
	cmd.Stderr = nil
	if err = cmd.Run(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

var pidRegex = regexp.MustCompile("\n\\s*(\\d+)")

// StillRunning verifies if the given information corresponds to a running
// process.  The information should come from CreatePIDFile.
func StillRunning(info []byte) (bool, error) {
	match := pidRegex.FindSubmatch(info)
	if match == nil {
		// Corrupt/invalid lockfile. Assume stale.
		return false, nil
	}
	pid, err := strconv.Atoi(string(match[1]))
	if err != nil {
		// Corrupt/invalid lockfile. Assume stale.
		return false, nil
	}
	// Go's os turns the standard errors into indecipherable ones,
	// so use syscall directly.
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
			return false, nil
		}
	}
	cmd := makePsCommand(pid)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return bytes.Equal(info, out), nil
}
