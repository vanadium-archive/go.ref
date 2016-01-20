// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package v23test defines Shell, a wrapper around gosh.Shell that provides
// Vanadium-specific functionality such as credentials management,
// StartRootMountTable, and StartSyncbase.
package v23test

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/envvar"
	"v.io/x/lib/gosh"
	"v.io/x/ref"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
)

const (
	envBinDir           = "V23_BIN_DIR"
	envChildOutputDir   = "TMPDIR"
	envShellTestProcess = "V23_SHELL_TEST_PROCESS"
	useAgent            = true
)

var envPrincipal string

func init() {
	if useAgent {
		envPrincipal = ref.EnvAgentPath
	} else {
		envPrincipal = ref.EnvCredentials
	}
}

// TODO(sadovsky):
// - Make StartRootMountTable and StartSyncbase fast, and change tests that
//   build no other binaries to be normal (non-integration) tests.
// - Eliminate test.V23Init() and either add v23test.Init() or have v23.Init()
//   check for an env var and perform test-specific configuration.
// - Switch to using the testing package's -test.short flag and eliminate
//   SkipUnlessRunningIntegrationTests, the -v23.tests flag, and the "jiri test"
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
	initSession(c.sh.t, res)
	return res
}

// WithCredentials returns a clone of this command, configured to use the given
// credentials.
func (c *Cmd) WithCredentials(cr *Credentials) *Cmd {
	res := c.Clone()
	res.Vars[envPrincipal] = cr.Handle
	return res
}

// Shell wraps gosh.Shell and provides Vanadium-specific functionality.
type Shell struct {
	*gosh.Shell
	Ctx *context.T
	t   *testing.T
	pm  principalManager
}

// Opts augments gosh.Opts with Vanadium-specific options. See gosh.Opts for
// field descriptions.
type Opts struct {
	Fatalf               func(format string, v ...interface{})
	Logf                 func(format string, v ...interface{})
	PropagateChildOutput bool
	ChildOutputDir       string
	BinDir               string
	// Ctx is the Vanadium context to use. If nil, NewShell will call v23.Init to
	// create a context.
	Ctx *context.T
}

var calledInitTestMain = false

// NewShell creates a new Shell. 't' may be nil. Use v23.GetPrincipal(sh.Ctx) to
// get the bound principal, if needed.
func NewShell(t *testing.T, opts Opts) *Shell {
	fillDefaults(t, &opts)

	// Note: gosh only requires gosh.InitMain to be called if the user uses
	// gosh.Shell.FuncCmd, while we require InitTestMain to be called whenever a
	// user calls NewShell with a non-nil *testing.T. We're stricter than gosh
	// because we want to make sure that the bin dir gets shared across tests.
	if t != nil && !calledInitTestMain {
		t.Fatal("must call v23test.TestMain or v23test.InitTestMain from TestMain")
		return nil
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
	// Filter out any v23test or credentials-related env vars coming from outside.
	// Note, we intentionally retain envChildOutputDir ("TMPDIR") and
	// envShellTestProcess, as these should be propagated downstream.
	if envChildOutputDir != "TMPDIR" { // sanity check
		panic(envChildOutputDir)
	}
	for _, key := range []string{envBinDir, ref.EnvCredentials, ref.EnvAgentPath} {
		delete(sh.Vars, key)
	}
	if sh.t != nil {
		sh.Vars[envShellTestProcess] = "1"
	}

	cleanup := true
	defer func() {
		if cleanup {
			sh.Cleanup()
		}
	}()

	if err := sh.initPrincipalManager(); err != nil {
		if _, ok := err.(handledError); !ok {
			sh.HandleError(err)
		}
		return sh
	}
	if err := sh.initCtx(opts.Ctx); err != nil {
		if _, ok := err.(handledError); !ok {
			sh.HandleError(err)
		}
		return sh
	}

	cleanup = false
	return sh
}

type handledError struct {
	error
}

// ForkCredentials creates a new Credentials (with a fresh principal) and
// blesses it with the given extensions and no caveats, using this principal's
// default blessings. Additionally, it calls SetDefaultBlessings.
func (sh *Shell) ForkCredentials(extensions ...string) *Credentials {
	sh.Ok()
	creds, err := newCredentials(sh.pm)
	if err != nil {
		sh.HandleError(err)
		return nil
	}
	if err := addDefaultBlessings(v23.GetPrincipal(sh.Ctx), creds.Principal, extensions...); err != nil {
		sh.HandleError(err)
		return nil
	}
	return creds
}

// ForkContext creates a new context with forked credentials.
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

// Cleanup cleans up all resources associated with this Shell.
// See gosh.Shell.Cleanup for detailed description.
func (sh *Shell) Cleanup() {
	// Run sh.Shell.Cleanup even if DebugSystemShell panics.
	defer sh.Shell.Cleanup()
	if sh.t != nil && sh.t.Failed() && test.IntegrationTestsDebugShellOnError {
		sh.DebugSystemShell()
	}
}

// InitTestMain is intended for developers with complex setup or teardown
// requirements; developers should generally use TestMain. InitTestMain must be
// called early on in TestMain, before m.Run is called. It calls gosh.InitMain
// and, if V23_BIN_DIR is not set, sets it to a new temporary directory. Returns
// a cleanup function that should be called after m.Run but before os.Exit.
func InitTestMain() func() {
	gosh.InitMain()
	calledInitTestMain = true
	cleanup := func() {}
	if os.Getenv(envBinDir) == "" {
		dir, err := ioutil.TempDir("", "bin-")
		if err != nil {
			panic(err)
		}
		cleanup = func() {
			os.RemoveAll(dir)
			os.Unsetenv(envBinDir)
		}
		if err := os.Setenv(envBinDir, dir); err != nil {
			cleanup()
			panic(err)
		}
	}
	return cleanup
}

// TestMain calls flag.Parse and does some v23test/gosh setup work, then calls
// os.Exit(m.Run()). Developers with complex setup or teardown requirements may
// need to use InitTestMain instead.
func TestMain(m *testing.M) {
	flag.Parse()
	var code int
	func() {
		defer InitTestMain()()
		code = m.Run()
	}()
	os.Exit(code)
}

// SkipUnlessRunningIntegrationTests should be called first thing inside of the
// test function body of an integration test. It causes this test to be skipped
// unless integration tests are being run, i.e. unless the -v23.tests flag is
// set.
// TODO(sadovsky): Switch to using -test.short. See TODO above.
func SkipUnlessRunningIntegrationTests(t *testing.T) {
	// Note: The "jiri test run vanadium-integration-test" command looks for test
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

func initSession(t *testing.T, c *Cmd) {
	c.S = expect.NewSession(t, c.StdoutPipe(), time.Minute)
	c.S.SetVerbosity(testing.Verbose())
}

func newCmd(sh *Shell, c *gosh.Cmd) *Cmd {
	res := &Cmd{Cmd: c, sh: sh}
	initSession(sh.t, res)
	res.Vars[envPrincipal] = sh.ForkCredentials("child").Handle
	return res
}

// Cmd returns a Cmd for an invocation of the named program. The given arguments
// are passed to the child as command-line arguments.
func (sh *Shell) Cmd(name string, args ...string) *Cmd {
	c := sh.Shell.Cmd(name, args...)
	if sh.Err != nil {
		return nil
	}
	return newCmd(sh, c)
}

// FuncCmd returns a Cmd for an invocation of the given registered Func. The
// given arguments are gob-encoded in the parent process, then gob-decoded in
// the child and passed to the Func. To specify command-line arguments for the
// child invocation, append to the returned Cmd's Args.
func (sh *Shell) FuncCmd(f *gosh.Func, args ...interface{}) *Cmd {
	c := sh.Shell.FuncCmd(f, args...)
	if sh.Err != nil {
		return nil
	}
	return newCmd(sh, c)
}

////////////////////////////////////////////////////////////////////////////////
// DebugSystemShell

// DebugSystemShell drops the user into a debug system shell (e.g. bash) that
// includes all environment variables from sh, and sets V23_BIN_DIR to
// sh.Opts.BinDir. If there is no controlling TTY, DebugSystemShell does
// nothing.
func (sh *Shell) DebugSystemShell() {
	// Make sure we have non-nil Fatalf and Logf functions.
	opts := Opts{Fatalf: sh.Opts.Fatalf, Logf: sh.Opts.Logf}
	fillDefaults(sh.t, &opts)
	fatalf, logf := opts.Fatalf, opts.Logf

	cwd, err := os.Getwd()
	if err != nil {
		fatalf("Getwd() failed: %v\n", err)
		return
	}

	// Transfer stdin, stdout, and stderr to the new process, and set target
	// directory for the system shell to start in.
	devtty := "/dev/tty"
	fd, err := syscall.Open(devtty, syscall.O_RDWR, 0)
	if err != nil {
		logf("WARNING: Open(%q) failed: %v\n", devtty, err)
		return
	}

	file := os.NewFile(uintptr(fd), devtty)
	attr := os.ProcAttr{
		Files: []*os.File{file, file, file},
		Dir:   cwd,
	}
	env := envvar.MergeMaps(envvar.SliceToMap(os.Environ()), sh.Vars)
	env[envPrincipal] = sh.ForkCredentials("debug").Handle
	env[envBinDir] = sh.Opts.BinDir
	attr.Env = envvar.MapToSlice(env)

	write := func(s string) {
		if _, err := file.WriteString(s); err != nil {
			fatalf("WriteString(%q) failed: %v\n", s, err)
			return
		}
	}

	write(">> Starting a new interactive shell\n")
	write(">> Hit CTRL-D to resume the test\n")

	shellPath := "/bin/sh"
	if shellPathFromEnv := os.Getenv("SHELL"); shellPathFromEnv != "" {
		shellPath = shellPathFromEnv
	}
	proc, err := os.StartProcess(shellPath, []string{}, &attr)
	if err != nil {
		fatalf("StartProcess(%q) failed: %v\n", shellPath, err)
		return
	}

	// Wait until the user exits the shell.
	state, err := proc.Wait()
	if err != nil {
		fatalf("Wait() failed: %v\n", err)
		return
	}

	write(fmt.Sprintf(">> Exited shell: %s\n", state.String()))
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

func (sh *Shell) initPrincipalManager() error {
	dir := sh.MakeTempDir()
	if sh.Err != nil {
		return handledError{sh.Err}
	}
	var pm principalManager
	if useAgent {
		var err error
		if pm, err = newAgentPrincipalManager(dir); err != nil {
			return err
		}
	} else {
		pm = newFilesystemPrincipalManager(dir)
	}
	sh.pm = pm
	return nil
}

func (sh *Shell) initCtx(ctx *context.T) error {
	if ctx == nil {
		var shutdown func()
		ctx, shutdown = v23.Init()
		if sh.AddToCleanup(shutdown); sh.Err != nil {
			return handledError{sh.Err}
		}
		if sh.t != nil {
			creds, err := newRootCredentials(sh.pm)
			if err != nil {
				return err
			}
			if ctx, err = v23.WithPrincipal(ctx, creds.Principal); err != nil {
				return err
			}
		}
	}
	if sh.t != nil {
		ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}}})
	}
	sh.Ctx = ctx
	return nil
}
