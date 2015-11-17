// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"runtime/debug"
	"syscall"

	"v.io/v23"
	"v.io/v23/syncbase"

	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

// StartSyncbased starts a syncbased process, intended to be accessed from an
// integration test (run using --v23.tests). The returned cleanup function
// should be called once the syncbased process is no longer needed.  See
// StartKillableSyncbased for killing the syncbase with an arbitrary signal.
func StartSyncbased(t *v23tests.T, creds *modules.CustomCredentials, name, rootDir, permsLiteral string) (cleanup func()) {
	f := StartKillableSyncbased(t, creds, name, rootDir, permsLiteral)
	return func() {
		f(syscall.SIGINT)
	}
}

// StartKillableSyncbased starts a syncbased process, intended to be accessed from an
// integration test (run using --v23.tests). The returned cleanup function
// should be called once the syncbased process is no longer needed.
func StartKillableSyncbased(t *v23tests.T, creds *modules.CustomCredentials,
	name, rootDir, permsLiteral string) (cleanup func(signal syscall.Signal)) {

	syncbased := t.BuildV23Pkg("v.io/x/ref/services/syncbase/syncbased")
	// Create root dir for the store.
	rmRootDir := false
	if rootDir == "" {
		var err error
		rootDir, err = ioutil.TempDir("", "syncbase_leveldb")
		if err != nil {
			V23Fatalf(t, "can't create temp dir: %v", err)
		}
		rmRootDir = true
	}

	// Start syncbased.
	invocation := syncbased.WithStartOpts(syncbased.StartOpts().WithCustomCredentials(creds)).Start(
		//"--vpath=vsync*=5",
		//"--alsologtostderr=true",
		"--v23.tcp.address=127.0.0.1:0",
		"--v23.permissions.literal", permsLiteral,
		"--name="+name,
		"--root-dir="+rootDir)
	RunClient(t, creds, runWaitForService, name)
	return func(signal syscall.Signal) {
		// TODO(sadovsky): Something's broken here. If the syncbased invocation
		// fails (e.g. if NewService returns an error), currently it's possible for
		// the test to fail without the crash error getting logged. This makes
		// debugging a challenge.
		go invocation.Kill(signal)
		stdout, stderr := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
		if err := invocation.Shutdown(stdout, stderr); err != nil {
			log.Printf("syncbased terminated with an error: %v\nstdout: %v\nstderr: %v\n", err, stdout, stderr)
		} else {
			// To debug sync (for example), uncomment this line as well as the --vpath
			// and --alsologtostderr lines above.
			// log.Printf("syncbased terminated cleanly\nstdout: %v\nstderr: %v\n", stdout, stderr)
		}
		if rmRootDir {
			if err := os.RemoveAll(rootDir); err != nil {
				V23Fatalf(t, "can't remove dir %v: %v", rootDir, err)
			}
		}
	}
}

// runWaitForService issues a noop rpc to force this process to wait until the
// server is ready to accept requests.  Without this, calls to glob will
// silently return no results if the service is not responding (i.e. ListApps,
// ListDatabases return the empty set).
// TODO(kash): sadovsky says that there is some other mechanism in the modules
// framework that doesn't involve RPCs, involving instrumenting the server to
// print some indication that it's ready (and detecting that from the parent
// process).
var runWaitForService = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()
	s := syncbase.NewService(args[0])
	a := s.App("dummyApp")
	if _, err := a.Exists(ctx); err != nil {
		return err
	}
	return nil
}, "runWaitForService")

// RunClient runs the given program and waits until it terminates.
func RunClient(t *v23tests.T, creds *modules.CustomCredentials, program modules.Program, args ...string) {
	client, err := t.Shell().StartWithOpts(
		t.Shell().DefaultStartOpts().WithCustomCredentials(creds),
		nil,
		program, args...)
	if err != nil {
		V23Fatalf(t, "unable to start the client: %v", err)
	}
	stdout, stderr := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
	if err := client.Shutdown(stdout, stderr); err != nil {
		V23Fatalf(t, "client failed: %v\nstdout: %v\nstderr: %v\n", err, stdout, stderr)
	}
}

func V23Fatalf(t *v23tests.T, format string, args ...interface{}) {
	debug.PrintStack()
	t.Fatalf(format, args...)
}
