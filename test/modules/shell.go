// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package modules implements a mechanism for running commonly used services as
// subprocesses, and client functionality for accessing those services.  Such
// services and functions are collectively called 'programs' and are managed by
// a 'Registry'. The Shell is analagous to the UNIX shell and maintains a key,
// value store of environment variables and config settings that are accessible
// to the programs that it hosts. Simple variable expansion is supported.
//
// Three types of 'programs' may be invoked via a Shell:
//
//   1) Functions of type Main as subprocesses via fork/exec.
//   2) Arbitrary non-Vanadium programs available on the underlying operating
//      system such as '/bin/cp', 'bash' etc.
//   3) Arbitrary Vanadium programs available on the underlying operating system
//      such as precompiled Vanadium services.
//
// The first type requires that the function to be executed is compiled into the
// binary executing the calls to the Shell.  These functions are registered with
// a single, per-process, registry.
//
// The second two types allow for arbitrary binaries to be executed. The
// distinction between a Vanadium and non-Vanadium program is that the Vanadium
// program implements the protocol used by v.io/x/ref/lib/exec package to
// synchronise between the parent and child processes and to share information
// such as the ConfigKey key,value store supported by the Shell, a shared
// secret, shared file descriptors etc.
//
// When the exec protocol is not used the only form of communication with the
// child processes are environment variables and command line flags and any
// shared file descriptors explicitly created by the parent process and expected
// by the child process; the Start method will not create any additional file
// descriptors.
//
// The registry provides the following functions:
//
//   Register: registers a Main function to be executed in a subprocess,
//     the returned Program is typically assigned to a global variable.
//   Dispatch: must be called in the child process to lookup and invoke the
//     requested function.  Typically called from TestMain.
//
// In all cases programs are started by invoking the StartWithOpts method on the
// Shell with the name of the program to run. An instance of the Handle
// interface is returned which can be used to interact with the function or
// subprocess, and in particular to read/write data from/to it using io channels
// that follow the stdin, stdout, stderr convention. The StartOpts struct is
// used to control the detailed behaviour of each such invocation.  Various
// helper functions are provided both for creating appropriate instances of
// StartOpts and for common uses of StartWithOpts.
//
// Each successful call to StartWithOpts returns a handle representing the
// running program. This handle can be used to gain access to that program's
// stdin, stdout, stderr and to request or synchronize with its termination via
// the Shutdown method. The Shutdown method can optionally be used to read any
// remaining output from the programs stdout and stderr.  The Shell maintains a
// record of all such handles and will call Shutdown on them in LIFO order when
// the Shell's Cleanup method is called.
//
// A simple protocol must be followed by all programs, in particular, they
// should wait for their stdin stream to be closed before exiting. The caller
// can then coordinate with any program by writing to that stdin stream and
// reading responses from the stdout stream, and it can close stdin when it's
// ready for the program to exit using the CloseStdin method on the program's
// handle. Any binary or script that follows this protocol can be used as well.
//
// By default, every Shell created by NewShell starts a security agent to manage
// principals for child processes. These default credentials can be overridden
// by passing a nil context to NewShell then specifying VeyronCredentials in the
// environment provided as a parameter to the StartWithOpts method. It is also
// possible to specify custom credentials via StartOpts.
//
// Interacting with Programs
//
// Handle.Stdout(), Stdin(), Stderr():
//
// StartWithOpts returns a Handle which can be used to interact with the running
// program. In particular, its Stdin() and Stdout() methods give access to the
// running process' corresponding stdin and stdout and hence can be used to
// communicate with it. Stderr is handled differently and is configured so that
// the child's stderr is written to a log file rather than a pipe. This is in
// order to maximise the liklihood of capturing stderr output from a crashed
// child process.
//
// Handle.Shutdown(stdout, stderr io.Writer):
//
// The Shutdown method is used to gracefully shutdown a program and to
// synchronise with its termination. In particular, Shutdown can be used to read
// any unread output from the program's stdout and stderr. Note that since
// Stderr is buffered to a file, Shutdown is able to return the entire contents
// of that file. This is useful for debugging misbehaving/crashing child
// processes.
//
// Shell.Cleanup(stdout, stderr io.Writer):
//
// The Shell keeps track of all Handles that it has issued and in particular if
// Shutdown (or Forget) have not been called, it will call Shutdown for each
// such Handle in LIFO order. This ensures that all programs will be Shutdown
// even if the developer does not explicitly take care to do so for every
// invocation.
//
// Pipes:
//
// StartWithOpts allows the caller to pass an io.Reader to the program
// (StartOpts.Stdin) for it to read from, rather than creating a new pipe
// internally. This makes it possible to connect the output of one program to
// the input of another directly.
//
// Command Line Arguments:
//
// The arguments passed in calls to Start are appended to any system required
// ones (e.g. for propagating test timeouts, verbosity etc) and the child
// process will call the program with the result of flag.Args(). In this way the
// caller can provide flags used by libraries in the child process as well as
// those specific to the program and the program will only receive the args
// specific to it. The usual "--" convention can be used to override this
// default behaviour.
//
// Caveats:
//
// Handle.Shutdown assumes that the child program/process will terminate when
// its stdin stream is closed. This assumption is unlikely to be valid for
// 'external' programs (e.g. /bin/cp) and in these cases Kill or some other
// application specific mechanism will need to be used.
package modules

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"v.io/x/lib/envvar"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/logging"
	"v.io/v23/security"

	"v.io/x/ref"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/exec"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/keymgr"
	"v.io/x/ref/test/expect"
)

const (
	shellBlessingExtension = "test-shell"

	defaultStartTimeout    = time.Minute
	defaultShutdownTimeout = time.Minute
	defaultExpectTimeout   = time.Minute
)

var defaultStartOpts = StartOpts{
	StartTimeout:    defaultStartTimeout,
	ShutdownTimeout: defaultShutdownTimeout,
	ExpectTimeout:   defaultExpectTimeout,
	ExecProtocol:    true,
}

// Shell represents the context within which programs are run.
type Shell struct {
	mu               sync.Mutex
	env              map[string]string
	handles          map[*execHandle]struct{}
	lifoHandles      []*execHandle
	defaultStartOpts StartOpts
	// tmpCredDir is the temporary directory created by this
	// shell. This must be removed when the shell is cleaned up.
	tempCredDir      string
	config           exec.Config
	principal        security.Principal
	agent            agent.KeyManager
	ctx              *context.T
	logger           logging.Logger
	sessionVerbosity bool
	cancelCtx        func()
}

// NewShell creates a new instance of Shell.
//
// If ctx is non-nil, the shell will manage Principals for child processes.
//
// If p is non-nil, any child process created has its principal blessed
// by the default blessings of 'p', Else any child process created has its
// principal blessed by the default blessings of ctx's principal.
//
// If verbosity is true additional debugging info will be displayed,
// in particular by the Shutdown.
//
// If t is non-nil, then the expect Session created for every invocation
// will be constructed with that value of t unless overridden by a
// StartOpts provided to that invocation. Providing a non-nil value of
// t enables expect.Session to call t.Error, Errorf and Log.
func NewShell(ctx *context.T, p security.Principal, verbosity bool, t expect.Testing) (*Shell, error) {
	sh := &Shell{
		env:              make(map[string]string),
		handles:          make(map[*execHandle]struct{}),
		config:           exec.NewConfig(),
		defaultStartOpts: defaultStartOpts,
		sessionVerbosity: verbosity,
	}
	sh.defaultStartOpts = sh.defaultStartOpts.WithSessions(t, time.Minute)
	if ctx == nil {
		sh.logger = logger.Global()
		return sh, nil
	}
	ctx, sh.cancelCtx = context.WithCancel(ctx)
	sh.ctx = ctx
	sh.logger = ctx
	var err error
	if sh.tempCredDir, err = ioutil.TempDir("", "sh_creds"); err != nil {
		return nil, err
	}
	if sh.agent, err = keymgr.NewLocalAgent(sh.tempCredDir, nil); err != nil {
		return nil, err
	}
	sh.principal = p
	if sh.principal == nil {
		sh.principal = v23.GetPrincipal(ctx)
	}
	return sh, nil
}

// DefaultStartOpts returns the current StartOpts stored with the Shell.
func (sh *Shell) DefaultStartOpts() StartOpts {
	return sh.defaultStartOpts
}

// SetDefaultStartOpts sets the default StartOpts stored with the Shell.
func (sh *Shell) SetDefaultStartOpts(opts StartOpts) {
	sh.defaultStartOpts = opts
}

// CustomCredentials encapsulates a Principal which can be shared with
// one or more processes run by a Shell.
type CustomCredentials struct {
	p    security.Principal
	path string
}

// Principal returns the Principal.
func (c *CustomCredentials) Principal() security.Principal {
	return c.p
}

// Path returns the path to the credential's agent.
// Typically you would pass this to a child process in EnvAgentPath.
func (c *CustomCredentials) Path() string {
	return c.path
}

// NewCustomCredentials creates a new Principal for StartWithOpts.
// Returns nil if the shell is not managing principals.
func (sh *Shell) NewCustomCredentials() (cred *CustomCredentials, err error) {
	// Create child principal.
	if sh.ctx == nil {
		return nil, nil
	}
	id, err := sh.agent.NewPrincipal(true)
	if err != nil {
		return nil, err
	}
	dir, err := ioutil.TempDir(sh.tempCredDir, "a")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "sock")
	if err := sh.agent.ServePrincipal(id, path); err != nil {
		return nil, err
	}
	p, err := agentlib.NewAgentPrincipalX(path)
	if err != nil {
		return nil, err
	}
	return &CustomCredentials{p, path}, nil
}

// NewChildCredentials creates a new principal, served via the security agent
// whose blessings are an extension of this shell's principal (with the
// provided caveats).
//
// All processes started by this shell will recognize the credentials created
// by this call.
//
// Returns nil if the shell is not managing principals.
//
// Since the Shell type is intended for tests, it is not required to provide
// caveats.  In production scenarios though, one must think long and hard
// before blessing anothing principal without any caveats.
func (sh *Shell) NewChildCredentials(extension string, caveats ...security.Caveat) (*CustomCredentials, error) {
	creds, err := sh.NewCustomCredentials()
	if creds == nil {
		return nil, err
	}
	return sh.AddToChildCredentials(creds, extension, caveats...)
}

func (sh *Shell) AddToChildCredentials(creds *CustomCredentials, extension string, caveats ...security.Caveat) (*CustomCredentials, error) {
	parent := sh.principal
	child := creds.p
	if len(caveats) == 0 {
		caveats = []security.Caveat{security.UnconstrainedUse()}
	}

	// Bless the child principal with blessings derived from the default blessings
	// of shell's principal.
	blessings, err := parent.Bless(child.PublicKey(), parent.BlessingStore().Default(), extension, caveats[0], caveats[1:]...)
	if err != nil {
		return nil, err
	}

	union, err := security.UnionOfBlessings(child.BlessingStore().Default(), blessings)
	if err != nil {
		return nil, err
	}

	if err := child.BlessingStore().SetDefault(union); err != nil {
		return nil, err
	}
	if _, err := child.BlessingStore().Set(union, security.AllPrincipals); err != nil {
		return nil, err
	}
	if err := security.AddToRoots(child, blessings); err != nil {
		return nil, err
	}

	return creds, nil
}

// Env represents the environment for Main functions.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Vars   map[string]string // Environment variables
}

// EnvFromOS returns a new Env based on the underlying OS.
func EnvFromOS() *Env {
	return &Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Vars:   envvar.SliceToMap(os.Environ()),
	}
}

// Main describes the entry-point function type for registered programs.
type Main func(env *Env, args ...string) error

// Start is shorthand for StartWithOpts(sh.DefaultStartOpts(), ...)
func (sh *Shell) Start(env []string, prog Program, args ...string) (Handle, error) {
	return sh.StartWithOpts(sh.DefaultStartOpts(), env, prog, args...)
}

// StartOpts represents the options that can be passed to the
// StartWithOpts method.
type StartOpts struct {
	// Error is set when creating/intializing instances of StartOpts
	// via one of the factory methods and returned when StartWithOpts
	// is called. This allows usage of of the form:
	//
	// err := sh.StartWithOpts(sh.DefaultStartOpts()...)
	//
	// as opposed to:
	//
	// opts, err := sh.DefaultStartOpts(....)
	// if err != nil {
	//     panic(...)
	// }
	// sh.StartWithOpts(opts, ....)
	Error error

	// Stdin, if non-nil, will be used as the stdin for the child process.
	// If this option is set, then the Stdin() method on the returned Handle
	// will return nil. The client of this API maintains ownership of stdin
	// and must close it, i.e. the shell will not do so.
	Stdin io.Reader
	// Credentials, if non-nil, will be used as the credentials for the
	// child process. If the creds are nil or the shell is not managing
	// principals, the credentials are ignored.
	Credentials *CustomCredentials
	// ExecProtocol indicates whether the child process is expected to
	// implement the v.io/x.ref/lib/exec parent/child protocol.
	// It should be set to false when running non-vanadium programs
	// (e.g. /bin/cp).
	ExecProtocol bool
	// External indicates if the program is an external process rather than
	// a Main function.
	External bool
	// StartTimeout specifies the amount of time to wait for the
	// child process to signal its correct intialization for Vanadium
	// processes that implement the exec parent/child protocol. It has no
	// effect if External is set to true.
	StartTimeout time.Duration
	// ShutdownTimeout specifics the amount of time to wait for the child
	// process to exit when the Shutdown method is called on that
	// child's handle.
	ShutdownTimeout time.Duration
	// ExpectTesting is used when creating an instance of expect.Session
	// to embed in Handle.
	ExpectTesting expect.Testing
	// ExpectTimeout is the timeout to use with expect.Session.
	ExpectTimeout time.Duration
}

// DefaultStartOpts returns an instance of Startops with the current default
// values. The defaults have values for timeouts, no credentials
// (StartWithOpts will then create credentials each time it is called),
// and with ExecProtocol set to true.
// This is expected to be the common use case.
func DefaultStartOpts() StartOpts {
	return defaultStartOpts
}

// WithCustomCredentials returns an instance of StartOpts with the specified
// credentials.
//
// All other options are set to the current defaults.
func (opts StartOpts) WithCustomCredentials(creds *CustomCredentials) StartOpts {
	opts.Credentials = creds
	return opts
}

// WithSessions returns a copy of opts with the specified expect.Testing and
// associated timeout.
func (opts StartOpts) WithSessions(t expect.Testing, timeout time.Duration) StartOpts {
	opts.ExpectTesting = t
	opts.ExpectTimeout = timeout
	return opts
}

// WithStdin returns a copy of opts with the specified Stdin io.Reader.
func (opts StartOpts) WithStdin(stdin io.Reader) StartOpts {
	opts.Stdin = stdin
	return opts
}

// NoExecProgram returns a copy of opts with the External option
// enabled and ExecProtocol disabled.
func (opts StartOpts) NoExecProgram() StartOpts {
	opts.External = true
	opts.ExecProtocol = false
	return opts
}

// ExternalProgram returns a copy of StartOpts with the
// External option enabled.
func (opts StartOpts) ExternalProgram() StartOpts {
	opts.External = true
	return opts
}

var (
	ErrNotRegistered        = errors.New("program not registered")
	ErrNoExecAndCustomCreds = errors.New("ExecProtocol set to false but this invocation is attempting to use custome credentials")
)

// StartWithOpts starts the specified program according to the supplied
// StartOpts and returns a Handle which can be used for interacting with
// that program.
//
// The environment variables for the program are set by merging variables
// from the OS environment, those in this Shell and those provided as a
// parameter to it. In general, it prefers values from its parameter over
// those from the Shell, over those from the OS. However, the VeyronCredentials
// and agent FdEnvVar variables will never use the value from the Shell or OS.
//
// If the shell is managing principals, the program is configured to
// connect to the shell's agent. Custom credentials may be specified
// via StartOpts. If the shell is not managing principals, set
// the VeyronCredentials environment variable in the 'env' parameter.
//
// The Shell tracks all of the Handles that it creates so that it can shut
// them down when asked to. The returned Handle may be non-nil even when an
// error is returned, in which case it may be used to retrieve any output
// from the failed program.
//
// StartWithOpts will return a valid handle for errors that occur during the
// child processes startup process. It is thus possible to call Shutdown
// to obtain the error output. Handle will be nil if the error is due to
// some other reason, such as failure to create pipes/files before starting
// the child process. A common use will therefore be:
//
//    h, err := sh.Start(env, "/bin/echo", "hello")
//    if err != nil {
//        if h != nil {
//            h.Shutdown(nil,os.Stderr)
//        }
//        t.Fatal(err)
//    }
func (sh *Shell) StartWithOpts(opts StartOpts, env []string, prog Program, args ...string) (Handle, error) {
	var err error
	if opts.Error != nil {
		return nil, opts.Error
	}

	var info *programInfo
	if opts.External {
		info = registry.getExternalProgram(prog)
	} else if info = registry.getProgram(prog); info == nil {
		return nil, ErrNotRegistered
	}

	if !opts.ExecProtocol && opts.Credentials != nil {
		return nil, ErrNoExecAndCustomCreds
	}

	if sh.ctx != nil && opts.ExecProtocol && opts.Credentials == nil {
		opts.Credentials, err = sh.NewChildCredentials("child")
		if err != nil {
			return nil, err
		}
	}

	var agentPath string
	if opts.Credentials != nil {
		agentPath = opts.Credentials.Path()
	}

	handle := info.factory()
	h, err := handle.start(sh, agentPath, &opts, sh.setupProgramEnv(env), sh.expand(args))
	if err != nil {
		return h, err
	}
	sh.mu.Lock()
	sh.handles[h] = struct{}{}
	sh.lifoHandles = append(sh.lifoHandles, h)
	sh.mu.Unlock()
	return h, nil
}

// ProgramEnvelope returns the command line and environment that would be used
// for running the subprocess if it were started with the specifed arguments.
func (sh *Shell) ProgramEnvelope(env []string, prog Program, args ...string) ([]string, []string) {
	info := registry.getProgram(prog)
	if info == nil {
		return []string{}, []string{}
	}
	return info.factory().envelope(sh, sh.setupProgramEnv(env), sh.expand(args))
}

// Forget tells the Shell to stop tracking the supplied Handle. This is
// generally used when the application wants to control the order that
// programs are shutdown in.
func (sh *Shell) Forget(h Handle) {
	sh.mu.Lock()
	if handle, ok := h.(*execHandle); ok {
		delete(sh.handles, handle)
	}
	sh.mu.Unlock()
}

func (sh *Shell) expand(args []string) []string {
	exp := []string{}
	for _, a := range args {
		if len(a) > 0 && a[0] == '$' {
			if v, present := sh.env[a[1:]]; present {
				exp = append(exp, v)
				continue
			}
		}
		exp = append(exp, a)
	}
	return exp
}

// GetVar returns the variable associated with the specified key
// and an indication of whether it is defined or not.
func (sh *Shell) GetVar(key string) (string, bool) {
	sh.mu.Lock()
	v, present := sh.env[key]
	sh.mu.Unlock()
	return v, present
}

// SetVar sets the value to be associated with key.
func (sh *Shell) SetVar(key, value string) {
	sh.mu.Lock()
	// TODO(cnicolaou): expand value
	sh.env[key] = value
	sh.mu.Unlock()
}

// ClearVar removes the speficied variable from the Shell's environment
func (sh *Shell) ClearVar(key string) {
	sh.mu.Lock()
	delete(sh.env, key)
	sh.mu.Unlock()
}

// GetConfigKey returns the value associated with the specified key in
// the Shell's config and an indication of whether it is defined or
// not.
func (sh *Shell) GetConfigKey(key string) (string, bool) {
	v, err := sh.config.Get(key)
	return v, err == nil
}

// SetConfigKey sets the value of the specified key in the Shell's
// config.
func (sh *Shell) SetConfigKey(key, value string) {
	sh.config.Set(key, value)
}

// ClearConfigKey removes the speficied key from the Shell's config.
func (sh *Shell) ClearConfigKey(key string) {
	sh.config.Clear(key)
}

// Env returns the entire set of environment variables associated with this
// Shell as a string slice.
func (sh *Shell) Env() []string {
	sh.mu.Lock()
	vars := envvar.MapToSlice(sh.env)
	sh.mu.Unlock()
	return vars
}

// Cleanup calls Shutdown on all of the Handles currently being tracked
// by the Shell and writes to stdout and stderr as per the Shutdown
// method in the Handle interface. Cleanup returns the error from the
// last Shutdown that returned a non-nil error. The order that the
// Shutdown routines are executed is not defined.
func (sh *Shell) Cleanup(stdout, stderr io.Writer) error {
	sh.mu.Lock()
	verbose := sh.sessionVerbosity
	sh.mu.Unlock()

	writeMsg := func(format string, args ...interface{}) {
		if !verbose {
			return
		}
		if stderr != nil {
			fmt.Fprintf(stderr, format, args...)
		}
	}

	writeMsg("---- Shell Cleanup ----\n")
	defer writeMsg("---- Shell Cleanup Complete ----\n")

	sh.mu.Lock()
	handles := make([]*execHandle, 0, len(sh.lifoHandles))
	for _, h := range sh.lifoHandles {
		if _, present := sh.handles[h]; present {
			handles = append(handles, h)
		}
	}
	sh.handles = make(map[*execHandle]struct{})
	sh.lifoHandles = nil
	sh.mu.Unlock()
	var err error
	for i := len(handles); i > 0; i-- {
		h := handles[i-1]
		writeMsg("---- Cleanup calling Shutdown on program %q\n", h.desc)
		cerr := h.Shutdown(stdout, stderr)
		if cerr != nil {
			err = cerr
		}
		fn := func() string {
			if cerr == nil {
				return ": done"
			} else {
				return ": error: " + err.Error()
			}
		}
		writeMsg("---- Shutdown on program %q%s\n", h.desc, fn())
	}

	if sh.cancelCtx != nil {
		writeMsg("---- Cleanup calling cancelCtx ----\n")
		// Note(ribrdb, caprita): This will shutdown the agents.  If there
		// were errors shutting down it is possible there could be child
		// processes still running, and stopping the agent may cause
		// additional failures.
		sh.cancelCtx()
	}
	os.RemoveAll(sh.tempCredDir)
	return err
}

func (sh *Shell) setupProgramEnv(env []string) []string {
	osmap := envvar.SliceToMap(os.Environ())
	evmap := envvar.SliceToMap(env)

	sh.mu.Lock()
	defer sh.mu.Unlock()
	m1 := envvar.MergeMaps(osmap, sh.env)
	// Clear any VeyronCredentials directory in m1 as we never
	// want the child to directly use the directory specified
	// by the shell's VeyronCredentials.
	delete(m1, ref.EnvCredentials)
	delete(m1, ref.EnvAgentPath)

	m2 := envvar.MergeMaps(m1, evmap)
	return envvar.MapToSlice(m2)
}

// ExpectSession is a subset of v.io/x/ref/tests/expect.Session's methods
// that are embedded in Handle.
type ExpectSession interface {
	Expect(expected string)
	ExpectEOF() error
	ExpectRE(pattern string, n int) [][]string
	ExpectSetEventuallyRE(expected ...string) [][]string
	ExpectSetRE(expected ...string) [][]string
	ExpectVar(name string) string
	Expectf(format string, args ...interface{})
	ReadAll() (string, error)
	ReadLine() string
	SetVerbosity(bool)
	Failed() bool
	Error() error
}

// Handle represents a running program.
type Handle interface {
	ExpectSession

	// Stdout returns a reader to the running program's stdout stream.
	Stdout() io.Reader

	// Stderr returns a reader to the running program's stderr
	// stream.
	Stderr() io.Reader

	// Stdin returns a writer to the running program's stdin. The
	// convention is for programs to wait for stdin to be closed before
	// they exit, thus the caller should close stdin when it wants the
	// program to exit cleanly.
	Stdin() io.Writer

	// CloseStdin closes stdin in a manner that avoids a data race
	// between any current readers on it.
	CloseStdin()

	// Shutdown closes the Stdin for the program and then reads output
	// from the program's stdout until it encounters EOF, waits for
	// the program to complete and then reads all of its stderr output.
	// The stdout and stderr contents are written to the corresponding
	// io.Writers if they are non-nil, otherwise the content is discarded.
	Shutdown(stdout, stderr io.Writer) error

	// Pid returns the pid of the process running the program
	Pid() int
}
