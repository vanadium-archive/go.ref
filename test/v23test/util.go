// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file defines helper functions for running specific Vanadium binaries
// using v23shell.Shell.

package v23test

import (
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"strings"

	"v.io/v23"
	"v.io/x/ref"
)

var syncbaseDebugArgs = flag.String("v23test-syncbase-debug-args", "", "args to add to syncbased invocations; if non-empty, a -log_dir will be created automatically for each invocation")

// TODO(sadovsky): Drop this hack once the TODOs in v23test.go are addressed.
func maybeAddTcpAddressFlag(sh *Shell, args *[]string) {
	if _, ok := sh.Vars[envShellTestProcess]; ok {
		*args = append(*args, "-v23.tcp.address=127.0.0.1:0")
	}
}

// StartRootMountTable builds and starts mounttabled and calls SetRoots. Returns
// a function that can be called to send a signal to the started process.
func (sh *Shell) StartRootMountTable(args ...string) func(sig os.Signal) {
	sh.Ok()
	path := sh.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	if sh.Err != nil {
		return nil
	}
	maybeAddTcpAddressFlag(sh, &args)
	cmd := sh.Cmd(path, args...)
	if sh.Err != nil {
		return nil
	}
	cmd.Start()
	name := cmd.S.ExpectVar("NAME")
	if name == "" {
		sh.HandleError(errors.New("mounttabled failed to start"))
		return nil
	}
	sh.Vars[ref.EnvNamespacePrefix] = name
	if err := v23.GetNamespace(sh.Ctx).SetRoots(name); err != nil {
		sh.HandleError(err)
		return nil
	}
	sh.Ctx.Infof("Started root mount table: %s", name)
	return cmd.Shutdown
}

// StartSyncbase builds and starts syncbased. If rootDir is empty, it makes a
// new root dir. Returns a function that can be called to send a signal to the
// started process.
// TODO(sadovsky): Maybe take a Permissions object instead of permsLiteral.
func (sh *Shell) StartSyncbase(c *Credentials, name, rootDir, permsLiteral string, args ...string) func(sig os.Signal) {
	sh.Ok()
	path := sh.BuildGoPkg("v.io/x/ref/services/syncbase/syncbased")
	if sh.Err != nil {
		return nil
	}
	if rootDir == "" {
		rootDir = sh.MakeTempDir()
		if sh.Err != nil {
			return nil
		}
	}
	args = append([]string{"-name=" + name, "-root-dir=" + rootDir, "-v23.permissions.literal=" + permsLiteral}, args...)

	if *syncbaseDebugArgs != "" {
		syncbaseLogDir, err := ioutil.TempDir("", name)
		if err != nil {
			sh.HandleError(err)
			return nil
		}
		sh.Ctx.Infof("syncbased -log_dir for %s: %s", name, syncbaseLogDir)
		debugArgs := append(strings.Fields(*syncbaseDebugArgs), "-log_dir="+syncbaseLogDir)
		args = append(args, debugArgs...)
	}

	maybeAddTcpAddressFlag(sh, &args)
	cmd := sh.Cmd(path, args...)
	if sh.Err != nil {
		return nil
	}
	if c != nil {
		cmd = cmd.WithCredentials(c)
	}
	cmd.Start()
	endpoint := cmd.S.ExpectVar("ENDPOINT")
	if endpoint == "" {
		sh.HandleError(errors.New("syncbased failed to start"))
		return nil
	}
	sh.Ctx.Infof("Started syncbase: %s", endpoint)
	return cmd.Shutdown
}
