// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package v23test defines Shell, a wrapper around gosh.Shell that provides
// Vanadium-specific functionality such as credentials management,
// StartRootMountTable, and StartSyncbase.
package v23test

// TODO(sadovsky): Add DebugSystemShell command.

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/gosh"
	"v.io/x/ref"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
)

const (
	envBinDir           = "V23_BIN_DIR"
	envChildOutputDir   = "TMPDIR"
	envShellTestProcess = "V23_SHELL_TEST_PROCESS"
)

// TODO(sadovsky):
// - Make StartRootMountTable and StartSyncbase fast, and change tests that
//   build no other binaries to be normal (non-integration) tests.
// - Eliminate test.V23Init() and either add v23test.Init() or have v23.Init()
//   check for an env var and perform test-specific configuration.
// - Switch to using the testing package's -test.short flag and eliminate
//   SkipUnlessRunningIntegrationTests, the -v23tests flag, and the "jiri test"
//   implementation that parses test code to identify integration tests.

// Cmd wraps gosh.Cmd and provides Vanadium-specific functionality.
type Cmd struct {
	*gosh.Cmd
	S  *expect.Session
	sh *Shell
}

// Clone returns a new Cmd with a copy of this Cmd's configuration.
func (c *Cmd) Clone() *Cmd {
	res := &Cmd{Cmd: c.Cmd.Clone(), sh: c.sh}
	res.S = expect.NewSession(c.sh.t, res.StdoutPipe(), time.Minute)
	return res
}

// WithCredentials returns a clone of this command, configured to use the given
// credentials.
func (c *Cmd) WithCredentials(cr *Credentials) *Cmd {
	res := c.Clone()
	res.Vars[ref.EnvCredentials] = cr.Handle
	return res
}

// Shell wraps gosh.Shell and provides Vanadium-specific functionality.
type Shell struct {
	*gosh.Shell
	Ctx         *context.T
	Credentials *Credentials
	t           *testing.T
}

// Opts augments gosh.Opts with Vanadium-specific options. See gosh.Opts for
// field descriptions.
type Opts struct {
	Fatalf               func(format string, v ...interface{})
	Logf                 func(format string, v ...interface{})
	PropagateChildOutput bool
	ChildOutputDir       string
	BinDir               string
}

var calledRun = false

// NewShell creates a new Shell. 't' may be nil. Use v23.GetPrincipal(sh.Ctx) to
// get the bound principal, if needed.
func NewShell(t *testing.T, opts Opts) *Shell {
	fillDefaults(t, &opts)

	if t != nil {
		if !calledRun {
			t.Fatal("must call v23test.Run(m.Run) from TestMain")
			return nil
		}

	}

	// Note: On error, NewShell returns a *Shell with Opts.Fatalf initialized to
	// simplify things for the caller.
	sh := &Shell{
		Shell: gosh.NewShell(gosh.Opts{
			Fatalf:               opts.Fatalf,
			Logf:                 opts.Logf,
			PropagateChildOutput: opts.PropagateChildOutput,
			ChildOutputDir:       opts.ChildOutputDir,
			BinDir:               opts.BinDir,
		}),
		t: t,
	}
	if sh.Err != nil {
		return sh
	}

	// Temporarily make Fatalf non-fatal so that we can safely call gosh.Shell
	// functions and clean up on any error.
	oldFatalf := sh.Opts.Fatalf
	sh.Opts.Fatalf = nil

	err := initCtxAndCredentials(t, sh)

	// Restore Fatalf, then call Cleanup followed by HandleError if there was an
	// error.
	sh.Err = nil
	sh.Opts.Fatalf = oldFatalf

	if err != nil {
		sh.Cleanup()
		sh.HandleError(err)
	}
	return sh
}

// ForkCredentials creates a new Credentials (with a fresh principal) and
// blesses it with the given extensions and no caveats, using this principal's
// default blessings. Additionally, it calls SetDefaultBlessings.
func (sh *Shell) ForkCredentials(extensions ...string) *Credentials {
	sh.Ok()
	res, err := sh.Credentials.Fork(extensions...)
	sh.HandleError(err)
	return res
}

// ForkContext creates a new Context with forked credentials.
func (sh *Shell) ForkContext(extensions ...string) *context.T {
	sh.Ok()
	c := sh.ForkCredentials(extensions...)
	if sh.Err != nil {
		return nil
	}
	ctx, err := v23.WithPrincipal(sh.Ctx, c.Principal)
	sh.HandleError(err)
	return ctx
}

// Run does some initialization work, then returns run(). Exported so that
// TestMain functions can simply call os.Exit(v23test.Run(m.Run)).
func Run(run func() int) int {
	gosh.MaybeRunFnAndExit()
	calledRun = true
	// Set up shared bin dir if V23_BIN_DIR is not already set. Note, this can't
	// be done in NewShell because we wouldn't be able to clean up after ourselves
	// after all tests have run.
	if dir := os.Getenv(envBinDir); len(dir) == 0 {
		if dir, err := ioutil.TempDir("", "bin-"); err != nil {
			panic(err)
		} else {
			os.Setenv(envBinDir, dir)
			defer os.Unsetenv(envBinDir)
			defer os.RemoveAll(dir)
		}
	}
	return run()
}

// SkipUnlessRunningIntegrationTests should be called first thing inside of the
// test function body of an integration test. It causes this test to be skipped
// unless integration tests are being run, i.e. unless the -v23.tests flag is
// set.
// TODO(sadovsky): Switch to using -test.short. See TODO above.
func SkipUnlessRunningIntegrationTests(t *testing.T) {
	// The "jiri test run vanadium-integration-test" command looks for test
	// function names that start with "TestV23", and runs "go test" for only those
	// packages containing at least one such test. That's how it avoids passing
	// the -v23.tests flag to packages for which the flag is not registered.
	name, err := callerName()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "TestV23") {
		t.Fatalf("integration test names must start with \"TestV23\": %s", name)
		return
	}
	if !test.IntegrationTestsEnabled {
		t.SkipNow()
	}
}

////////////////////////////////////////////////////////////////////////////////
// Methods for starting subprocesses

func newCmd(sh *Shell, c *gosh.Cmd) *Cmd {
	res := &Cmd{Cmd: c, sh: sh}
	res.S = expect.NewSession(sh.t, res.StdoutPipe(), time.Minute)
	res.Vars[ref.EnvCredentials] = sh.ForkCredentials("child").Handle
	return res
}

// Cmd returns a Cmd for an invocation of the named program.
func (sh *Shell) Cmd(name string, args ...string) *Cmd {
	c := sh.Shell.Cmd(name, args...)
	if sh.Err != nil {
		return nil
	}
	return newCmd(sh, c)
}

// Fn returns a Cmd for an invocation of the given registered Fn.
func (sh *Shell) Fn(fn *gosh.Fn, args ...interface{}) *Cmd {
	c := sh.Shell.Fn(fn, args...)
	if sh.Err != nil {
		return nil
	}
	return newCmd(sh, c)
}

// Main returns a Cmd for an invocation of the given registered main() function.
// Intended usage: Have your program's main() call RealMain, then write a parent
// program that uses Shell.Main to run RealMain in a child process. With this
// approach, RealMain can be compiled into the parent program's binary. Caveat:
// potential flag collisions.
func (sh *Shell) Main(fn *gosh.Fn, args ...string) *Cmd {
	c := sh.Shell.Main(fn, args...)
	if sh.Err != nil {
		return nil
	}
	return newCmd(sh, c)
}

////////////////////////////////////////////////////////////////////////////////
// Internals

func callerName() (string, error) {
	pc, _, _, ok := runtime.Caller(2)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	name := runtime.FuncForPC(pc).Name()
	// Strip package path.
	return name[strings.LastIndex(name, ".")+1:], nil
}

func fillDefaults(t *testing.T, opts *Opts) {
	if opts.Fatalf == nil {
		if t != nil {
			opts.Fatalf = func(format string, v ...interface{}) {
				debug.PrintStack()
				t.Fatalf(format, v...)
			}
		} else {
			opts.Fatalf = func(format string, v ...interface{}) {
				panic(fmt.Sprintf(format, v...))
			}
		}
	}
	if opts.Logf == nil {
		if t != nil {
			opts.Logf = t.Logf
		} else {
			opts.Logf = log.Printf
		}
	}
	if opts.ChildOutputDir == "" {
		opts.ChildOutputDir = os.Getenv(envChildOutputDir)
	}
	if opts.BinDir == "" {
		opts.BinDir = os.Getenv(envBinDir)
	}
}

func initCtxAndCredentials(t *testing.T, sh *Shell) error {
	// Create context.
	ctx, shutdown := v23.Init()
	sh.AddToCleanup(shutdown)
	if err := sh.Err; err != nil {
		return err
	}
	if t != nil {
		ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}}})
		sh.Vars[envShellTestProcess] = "1"
	}

	// Create principal and update context.
	dir := sh.MakeTempDir()
	if err := sh.Err; err != nil {
		return err
	}
	creds, err := newRootCredentials(newFilesystemPrincipalManager(dir))
	if err != nil {
		return err
	}
	ctx, err = v23.WithPrincipal(ctx, creds.Principal)
	if err != nil {
		return err
	}

	sh.Ctx = ctx
	sh.Credentials = creds
	return nil
}
