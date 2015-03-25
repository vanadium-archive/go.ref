// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux darwin

package impl

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// Chown is only availabe on UNIX platforms so this file has a build
// restriction.
func (hw *WorkParameters) Chown() error {
	chown := func(path string, _ os.FileInfo, inerr error) error {
		if inerr != nil {
			return inerr
		}
		if hw.dryrun {
			log.Printf("[dryrun] os.Chown(%s, %d, %d)", path, hw.uid, hw.gid)
			return nil
		}
		return os.Chown(path, hw.uid, hw.gid)
	}

	for _, p := range []string{hw.workspace, hw.logDir} {
		// TODO(rjkroege): Ensure that the device manager can read log entries.
		if err := filepath.Walk(p, chown); err != nil {
			return fmt.Errorf("os.Chown(%s, %d, %d) failed: %v", p, hw.uid, hw.gid, err)
		}
	}
	return nil
}

func (hw *WorkParameters) Exec() error {
	attr := new(syscall.ProcAttr)

	if dir, err := os.Getwd(); err != nil {
		log.Printf("error Getwd(): %v\n", err)
		return fmt.Errorf("os.Getwd failed: %v", err)
		attr.Dir = dir
	}
	attr.Env = hw.envv
	attr.Files = []uintptr{
		uintptr(syscall.Stdin),
		uintptr(syscall.Stdout),
		uintptr(syscall.Stderr),
	}

	attr.Sys = new(syscall.SysProcAttr)
	attr.Sys.Setsid = true
	if hw.dryrun {
		log.Printf("[dryrun] syscall.Setgid(%d)", hw.gid)
		log.Printf("[dryrun] syscall.Setuid(%d)", hw.uid)
	} else {
		attr.Sys.Credential = new(syscall.Credential)
		attr.Sys.Credential.Gid = uint32(hw.gid)
		attr.Sys.Credential.Uid = uint32(hw.uid)
	}

	_, _, err := syscall.StartProcess(hw.argv0, hw.argv, attr)
	if err != nil {
		if !hw.dryrun {
			log.Printf("StartProcess failed: attr: %#v, attr.Sys: %#v, attr.Sys.Cred: %#v error: %v\n", attr, attr.Sys, attr.Sys.Credential, err)
		} else {
			log.Printf("StartProcess failed: %v", err)
		}
		return fmt.Errorf("syscall.StartProcess(%s) failed: %v", hw.argv0, err)
	}
	// TODO(rjkroege): Return the pid to the node manager.
	os.Exit(0)
	return nil // Not reached.
}

func (hw *WorkParameters) Remove() error {
	for _, p := range hw.argv {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("os.RemoveAll(%s) failed: %v", p, err)
		}
	}
	return nil
}
