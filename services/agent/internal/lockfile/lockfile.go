// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lockfile provides methods to associate process ids (PIDs) with a file.
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

const (
	lockSuffix = "lock"
	tempSuffix = "templock"
)

// CreateLockfile associates the provided file with the process ID of the
// caller.
//
// file should not contain any useful content because CreateLockfile may
// delete and recreate it.
//
// Only one active process can be associated with a file at a time. Thus, if
// another active process is currently associated with file, then
// CreateLockfile will return an error.
func CreateLockfile(file string) error {
	f, err := ioutil.TempFile(filepath.Dir(file), filepath.Base(file)+"-"+tempSuffix)
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
	lockfile := file + "-" + lockSuffix
	err = os.Link(f.Name(), lockfile)
	if err == nil {
		return nil
	}

	// Check for a stale lock
	lockProcessInfo, err := ioutil.ReadFile(lockfile)
	if err != nil {
		return err
	}
	if err, running := stillRunning(lockProcessInfo); running {
		return fmt.Errorf("process is already running:\n%s", lockProcessInfo)
	} else if err != nil {
		return err
	}

	// Delete the file if the old process left one behind.
	if err = os.Remove(file); !os.IsNotExist(err) {
		return err
	}

	// Note(ribrdb): There's a race here between checking the file contents
	// and deleting the file, but I don't think it will be an issue in normal
	// usage.
	return os.Rename(f.Name(), lockfile)
}

var pidRegex = regexp.MustCompile("\n\\s*(\\d+)")

func stillRunning(expected []byte) (error, bool) {
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

// RemoveLockfile removes file and the corresponding lock on it.
func RemoveLockfile(file string) {
	path := file + "-" + lockSuffix
	if err := os.Remove(path); err != nil {
		vlog.Infof("Unable to remove lockfile %q: %v", path, err)
	}
	if err := os.Remove(file); err != nil {
		vlog.Infof("Unable to remove %q: %v", path, err)
	}
}

func makePsCommand(pid int) *exec.Cmd {
	return exec.Command("ps", "-o", "pid,ppid,lstart,user,comm", "-p", strconv.Itoa(pid))
}
