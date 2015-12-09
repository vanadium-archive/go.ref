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
	"time"

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

// StartKillableSyncbased starts a syncbased process, intended to be accessed
// from an integration test (run using --v23.tests). The returned cleanup
// function should be called once the syncbased process is no longer needed.
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

	// Start syncbased. Run with --dev to enable development mode methods such as
	// DevModeUpdateVClock.
	invocation := syncbased.WithStartOpts(syncbased.StartOpts().WithCustomCredentials(creds).WithSessions(t, 5*time.Second)).Start(
		//"--v=5",
		//"--vpath=vsync*=5",
		//"--alsologtostderr=true",
		"--v23.tcp.address=127.0.0.1:0",
		"--v23.permissions.literal", permsLiteral,
		"--name="+name,
		"--root-dir="+rootDir,
		"--dev")

	cleanup = func(signal syscall.Signal) {
		go invocation.Kill(signal)
		stdout, stderr := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
		if err := invocation.Shutdown(stdout, stderr); err != nil {
			log.Printf("syncbased terminated with an error: %v\nstdout: %v\nstderr: %v\n", err, stdout, stderr)
		} else {
			// To debug sync (for example), uncomment this line as well as the logging
			// flags in the invocation above.
			//log.Printf("syncbased terminated cleanly\nstdout: %v\nstderr: %v\n", stdout, stderr)
		}
		if rmRootDir {
			if err := os.RemoveAll(rootDir); err != nil {
				V23Fatalf(t, "failed to remove dir %v: %v", rootDir, err)
			}
		}
	}

	// Wait for syncbased to start.  If syncbased fails to start, this will time
	// out after 5 seconds and return "".
	endpoint := invocation.ExpectVar("ENDPOINT")
	if endpoint == "" {
		cleanup(syscall.SIGKILL)
		t.Fatalf("syncbased failed to start")
	}
	return cleanup
}

// RunClient runs the given program and waits for it to terminate.
// TODO(sadovsky): This function will soon go away. Do not depend on it.
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
