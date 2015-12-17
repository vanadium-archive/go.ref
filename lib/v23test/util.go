// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file defines helper functions for running specific Vanadium binaries
// using v23shell.Shell.
// TODO(sadovsky): Maybe make these functions use Shell.Main instead of
// JiriBuildGoPkg plus Shell.Cmd. This will speed things up substantially, but
// might not work due to flag collisions.

package v23test

import (
	"errors"
	"os"
	"time"

	"v.io/v23"
	"v.io/x/ref"
	"v.io/x/ref/test/expect"
)

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
	path := sh.JiriBuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	if sh.Err != nil {
		return nil
	}
	maybeAddTcpAddressFlag(sh, &args)
	cmd := sh.Cmd(path, args...)
	if sh.Err != nil {
		return nil
	}
	session := expect.NewSession(nil, cmd.StdoutPipe(), time.Minute)
	cmd.Start()
	name := session.ExpectVar("NAME")
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
	path := sh.JiriBuildGoPkg("v.io/x/ref/services/syncbase/syncbased")
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
	maybeAddTcpAddressFlag(sh, &args)
	cmd := sh.Cmd(path, args...)
	if sh.Err != nil {
		return nil
	}
	if c != nil {
		cmd = cmd.WithCredentials(c)
	}
	session := expect.NewSession(nil, cmd.StdoutPipe(), time.Minute)
	cmd.Start()
	endpoint := session.ExpectVar("ENDPOINT")
	if endpoint == "" {
		sh.HandleError(errors.New("syncbased failed to start"))
		return nil
	}
	sh.Ctx.Infof("Started syncbase: %s", endpoint)
	return cmd.Shutdown
}
