// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lockutil contains utilities for building file locks.
package lockutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
)

const (
	// Should be incremented each time there is a change to the contents of
	// the lock file.
	currentVersion uint32 = 0
	// Should not change across versions.
	versionLabel = "VERSION"
)

func CreateLockFile(dir, file string) (string, error) {
	f, err := ioutil.TempFile(dir, file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(fmt.Sprintf("%s:%d\n", versionLabel, currentVersion)); err != nil {
		return "", err
	}
	switch currentVersion {
	case 0:
		err = createV0(f)
	case 1:
		err = createV1(f)
	default:
		return "", fmt.Errorf("unknown version: %d", currentVersion)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// StillHeld verifies if the given lock information corresponds to a running
// process.
func StillHeld(info []byte) (bool, error) {
	eol := bytes.IndexByte(info, '\n')
	if eol == -1 {
		return false, fmt.Errorf("couldn't parse version from %s", string(info))
	}
	prefix := []byte(versionLabel + ":")
	if !bytes.HasPrefix(info, prefix) {
		return false, fmt.Errorf("couldn't parse version from %s", string(info))
	}
	version, err := strconv.ParseUint(string(bytes.TrimPrefix(info[:eol], prefix)), 10, 32)
	if err != nil {
		return false, fmt.Errorf("couldn't parse version from %s: %v", string(info), err)
	}
	info = info[eol+1:]
	switch version {
	case 0:
		return stillHeldV0(info)
	case 1:
		return stillHeldV1(info)
	default:
		return false, fmt.Errorf("unknown version: %d", version)
	}
}
