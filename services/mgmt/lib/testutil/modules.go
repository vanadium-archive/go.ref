// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"

	"v.io/x/ref/envvar"
	mounttable "v.io/x/ref/services/mounttable/lib"
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

func init() {
	modules.RegisterChild("rootMT", ``, rootMT)
}

func rootMT(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	lspec := v23.GetListenSpec(ctx)
	server, err := v23.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	mt, err := mounttable.NewMountTableDispatcher("")
	if err != nil {
		return fmt.Errorf("mounttable.NewMountTableDispatcher failed: %s", err)
	}
	eps, err := server.Listen(lspec)
	if err != nil {
		return fmt.Errorf("server.Listen failed: %s", err)
	}
	if err := server.ServeDispatcher("", mt); err != nil {
		return fmt.Errorf("root failed: %s", err)
	}
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	for _, ep := range eps {
		fmt.Fprintf(stdout, "MT_NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(stdin)
	return nil
}

// startRootMT sets up a root mount table for tests.
func startRootMT(t *testing.T, sh *modules.Shell) (string, modules.Handle) {
	h, err := sh.Start("rootMT", nil, "--v23.tcp.address=127.0.0.1:0")
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
		t.Fatalf(testutil.FormatLogLine(3, "SetRoots(%v) failed with %v", roots, err))
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
	sh.ClearVar(envvar.Credentials)

	mtName, mtHandle := startRootMT(t, sh)
	vlog.VI(1).Infof("Started shell mounttable with name %v", mtName)
	// Make sure the root mount table is the last process to be shutdown
	// since the others will likely want to communicate with it during
	// their shutdown process
	sh.Forget(mtHandle)

	// TODO(caprita): Define a GetNamespaceRootsCommand in modules/core and
	// use that?

	oldNamespaceRoots := v23.GetNamespace(ctx).Roots()
	fn := func() {
		vlog.VI(1).Info("------------ CLEANUP ------------")
		vlog.VI(1).Info("---------------------------------")
		vlog.VI(1).Info("--(cleaning up shell)------------")
		if err := sh.Cleanup(os.Stdout, os.Stderr); err != nil {
			t.Fatalf(testutil.FormatLogLine(2, "sh.Cleanup failed with %v", err))
		}
		vlog.VI(1).Info("--(done cleaning up shell)-------")
		vlog.VI(1).Info("--(shutting down root mt)--------")
		if err := mtHandle.Shutdown(os.Stdout, os.Stderr); err != nil {
			t.Fatalf(testutil.FormatLogLine(2, "mtHandle.Shutdown failed with %v", err))
		}
		vlog.VI(1).Info("--(done shutting down root mt)---")
		vlog.VI(1).Info("--------- DONE CLEANUP ----------")
		setNSRoots(t, ctx, oldNamespaceRoots...)
	}
	setNSRoots(t, ctx, mtName)
	sh.SetVar(envvar.NamespacePrefix, mtName)
	return sh, fn
}

// RunCommand runs a modules command.
func RunCommand(t *testing.T, sh *modules.Shell, env []string, cmd string, args ...string) modules.Handle {
	h, err := sh.StartWithOpts(sh.DefaultStartOpts(), env, cmd, args...)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "failed to start %q: %s", cmd, err))
		return nil
	}
	h.SetVerbosity(testing.Verbose())
	return h
}

// NewServer creates a new server.
func NewServer(ctx *context.T) (rpc.Server, string) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	spec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	endpoints, err := server.Listen(spec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", spec, err)
	}
	return server, endpoints[0].String()
}

// ReadPID waits for the "ready:<PID>" line from the child and parses out the
// PID of the child.
func ReadPID(t *testing.T, h modules.ExpectSession) int {
	m := h.ExpectRE("ready:([0-9]+)", -1)
	if len(m) == 1 && len(m[0]) == 2 {
		pid, err := strconv.Atoi(m[0][1])
		if err != nil {
			t.Fatalf(testutil.FormatLogLine(2, "Atoi(%q) failed: %v", m[0][1], err))
		}
		return pid
	}
	t.Fatalf(testutil.FormatLogLine(2, "failed to extract pid: %v", m))
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
		vlog.Fatalf("EvalSymlinks(%v) failed: %v", rootDir, err)
	}

	return rootDir, func() {
		if t.Failed() || os.Getenv(preserveWorkspaceEnv) != "" {
			t.Logf("You can examine the %s workspace at %v", prefix, rootDir)
		} else {
			os.RemoveAll(rootDir)
		}
	}
}
