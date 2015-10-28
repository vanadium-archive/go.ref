// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package servicetest

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/x/ref"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

const (
	// Setting this environment variable to any non-empty value avoids
	// removing the generated workspace for successful test runs (for
	// failed test runs, this is already the case).  This is useful when
	// developing test cases.
	preserveWorkspaceEnv = "V23_TEST_PRESERVE_WORKSPACE"
)

var rootMT = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	mt, err := mounttablelib.NewMountTableDispatcher(ctx, "", "", "mounttable")
	if err != nil {
		return fmt.Errorf("mounttablelib.NewMountTableDispatcher failed: %s", err)
	}
	ctx, server, err := v23.WithNewDispatchingServer(ctx, "", mt, options.ServesMountTable(true))
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	fmt.Fprintf(env.Stdout, "PID=%d\n", os.Getpid())
	for _, ep := range server.Status().Endpoints {
		fmt.Fprintf(env.Stdout, "MT_NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(env.Stdin)
	return nil
}, "rootMT")

// startRootMT sets up a root mount table for tests.
func startRootMT(t *testing.T, sh *modules.Shell) (string, modules.Handle) {
	h, err := sh.Start(nil, rootMT, "--v23.tcp.address=127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start root mount table: %s", err)
	}
	h.ExpectVar("PID")
	rootName := h.ExpectVar("MT_NAME")
	if t.Failed() {
		t.Fatalf("failed to read mt name: %s", h.Error())
	}
	return rootName, h
}

// setNSRoots sets the roots for the local runtime's namespace.
func setNSRoots(t *testing.T, ctx *context.T, roots ...string) {
	ns := v23.GetNamespace(ctx)
	if err := ns.SetRoots(roots...); err != nil {
		t.Fatal(testutil.FormatLogLine(3, "SetRoots(%v) failed with %v", roots, err))
	}
}

// CreateShellAndMountTable builds a new modules shell and its
// associated mount table.
func CreateShellAndMountTable(t *testing.T, ctx *context.T, p security.Principal) (*modules.Shell, func()) {
	sh, err := modules.NewShell(ctx, p, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts.ExpectTimeout = ExpectTimeout
	sh.SetDefaultStartOpts(opts)
	// The shell, will, by default share credentials with its children.
	sh.ClearVar(ref.EnvCredentials)

	mtName, _ := startRootMT(t, sh)
	ctx.VI(1).Infof("Started shell mounttable with name %v", mtName)

	// TODO(caprita): Define a GetNamespaceRootsCommand in modules/core and
	// use that?

	oldNamespaceRoots := v23.GetNamespace(ctx).Roots()
	fn := func() {
		ctx.VI(1).Info("------------ CLEANUP ------------")
		ctx.VI(1).Info("---------------------------------")
		ctx.VI(1).Info("--(cleaning up shell)------------")
		if err := sh.Cleanup(os.Stdout, os.Stderr); err != nil {
			t.Error(testutil.FormatLogLine(2, "sh.Cleanup failed with %v", err))
		}
		ctx.VI(1).Info("--(done cleaning up shell)-------")
		setNSRoots(t, ctx, oldNamespaceRoots...)
	}
	setNSRoots(t, ctx, mtName)
	sh.SetVar(ref.EnvNamespacePrefix, mtName)
	return sh, fn
}

// CreateShell builds a new modules shell.
func CreateShell(t *testing.T, ctx *context.T, p security.Principal) (*modules.Shell, func()) {
	sh, err := modules.NewShell(ctx, p, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts.ExpectTimeout = ExpectTimeout
	sh.SetDefaultStartOpts(opts)
	// The shell, will, by default share credentials with its children.
	sh.ClearVar(ref.EnvCredentials)

	fn := func() {
		ctx.VI(1).Info("------------ CLEANUP ------------")
		ctx.VI(1).Info("---------------------------------")
		ctx.VI(1).Info("--(cleaning up shell)------------")
		if err := sh.Cleanup(os.Stdout, os.Stderr); err != nil {
			t.Error(testutil.FormatLogLine(2, "sh.Cleanup failed with %v", err))
		}
		ctx.VI(1).Info("--(done cleaning up shell)-------")
	}
	nsRoots := v23.GetNamespace(ctx).Roots()
	if len(nsRoots) == 0 {
		t.Fatalf("shell context has no namespace roots")
	}
	sh.SetVar(ref.EnvNamespacePrefix, nsRoots[0])
	return sh, fn
}

// RunCommand runs a modules command.
func RunCommand(t *testing.T, sh *modules.Shell, env []string, prog modules.Program, args ...string) modules.Handle {
	h, err := sh.StartWithOpts(sh.DefaultStartOpts(), env, prog, args...)
	if err != nil {
		t.Fatal(testutil.FormatLogLine(2, "failed to start %q: %s", prog, err))
		return nil
	}
	h.SetVerbosity(testing.Verbose())
	return h
}

// ReadPID waits for the "ready:<PID>" line from the child and parses out the
// PID of the child.
func ReadPID(t *testing.T, h modules.ExpectSession) int {
	m := h.ExpectRE("ready:([0-9]+)", -1)
	if len(m) == 1 && len(m[0]) == 2 {
		pid, err := strconv.Atoi(m[0][1])
		if err != nil {
			t.Fatal(testutil.FormatLogLine(2, "Atoi(%q) failed: %v", m[0][1], err))
		}
		return pid
	}
	t.Fatal(testutil.FormatLogLine(2, "failed to extract pid: %v", m))
	return 0
}

// SetupRootDir sets up and returns a directory for the root and returns
// a cleanup function.
func SetupRootDir(t *testing.T, prefix string) (string, func()) {
	rootDir, err := ioutil.TempDir("", prefix)
	if err != nil {
		t.Fatalf("Failed to set up temporary dir for test: %v", err)
	}
	// On some operating systems (e.g. darwin) os.TempDir() can return a
	// symlink. To avoid having to account for this eventuality later,
	// evaluate the symlink.
	rootDir, err = filepath.EvalSymlinks(rootDir)
	if err != nil {
		logger.Global().Fatalf("EvalSymlinks(%v) failed: %v", rootDir, err)
	}

	return rootDir, func() {
		if t.Failed() || os.Getenv(preserveWorkspaceEnv) != "" {
			t.Logf("You can examine the %s workspace at %v", prefix, rootDir)
		} else {
			os.RemoveAll(rootDir)
		}
	}
}
