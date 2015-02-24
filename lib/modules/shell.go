// Package modules provides a mechanism for running commonly used services
// as subprocesses and client functionality for accessing those services.
// Such services and functions are collectively called 'commands' and are
// registered with a single, per-process, registry, but executed within a
// context, defined by the Shell type. The Shell is analagous to the original
// UNIX shell and maintains a key, value store of variables that is accessible
// to all of the commands that it hosts. These variables may be referenced by
// the arguments passed to commands.
//
// Commands are added to the registry in two ways:
// - via RegisterChild for a subprocess
// - via RegisterFunction for an in-process function
//
// In all cases commands are started by invoking the Start method on the
// Shell with the name of the command to run. An instance of the Handle
// interface is returned which can be used to interact with the function
// or subprocess, and in particular to read/write data from/to it using io
// channels that follow the stdin, stdout, stderr convention.
//
// A simple protocol must be followed by all commands, namely, they
// should wait for their stdin stream to be closed before exiting. The
// caller can then coordinate with any command by writing to that stdin
// stream and reading responses from the stdout stream, and it can close
// stdin when it's ready for the command to exit using the CloseStdin method
// on the command's handle.
//
// The signature of the function that implements the command is the
// same for both types of command and is defined by the Main function type.
// In particular stdin, stdout and stderr are provided as parameters, as is
// a map representation of the shell's environment.
//
// By default, every Shell created by NewShell starts a security agent
// to manage principals for child processes.  These default
// credentials can be overridden by passing a nil context to NewShell
// then specifying VeyronCredentials in the environment provided as a
// parameter to the Start method.
package modules

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"syscall"
	"time"

	"v.io/v23/security"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/security/agent"
	"v.io/core/veyron/security/agent/keymgr"
	"v.io/v23"
	"v.io/v23/context"
)

const (
	shellBlessingExtension = "test-shell"
	childBlessingExtension = "child"
)

// Shell represents the context within which commands are run.
type Shell struct {
	mu      sync.Mutex
	env     map[string]string
	handles map[Handle]struct{}
	// tmpCredDir is the temporary directory created by this
	// shell. This must be removed when the shell is cleaned up.
	tempCredDir               string
	startTimeout, waitTimeout time.Duration
	config                    exec.Config
	principal                 security.Principal
	agent                     *keymgr.Agent
	ctx                       *context.T
	cancelCtx                 func()
}

// NewShell creates a new instance of Shell.
//
// If ctx is non-nil, the shell will manage Principals for child processes.
//
// If p is non-nil, any child process created has its principal blessed
// by the default blessings of 'p', Else any child process created has its
// principal blessed by the default blessings of ctx's principal.
func NewShell(ctx *context.T, p security.Principal) (*Shell, error) {
	sh := &Shell{
		env:          make(map[string]string),
		handles:      make(map[Handle]struct{}),
		startTimeout: time.Minute,
		waitTimeout:  10 * time.Second,
		config:       exec.NewConfig(),
	}
	if ctx == nil {
		return sh, nil
	}
	var err error
	ctx, sh.cancelCtx = context.WithCancel(ctx)
	if ctx, err = v23.SetNewStreamManager(ctx); err != nil {
		return nil, err
	}
	sh.ctx = ctx

	if sh.tempCredDir, err = ioutil.TempDir("", "shell_credentials"); err != nil {
		return nil, err
	}
	if sh.agent, err = keymgr.NewLocalAgent(ctx, sh.tempCredDir, nil); err != nil {
		return nil, err
	}
	sh.principal = p
	if sh.principal == nil {
		sh.principal = v23.GetPrincipal(ctx)
	}
	return sh, nil
}

// NewChildCredentials creates a new principal, served via the
// security agent, that have a blessing from this shell's principal.
// All processes started by this shell will have access to such a
// principal.
func (sh *Shell) NewChildCredentials() (c *os.File, err error) {
	if sh.ctx == nil {
		return nil, nil
	}

	// Create child principal.
	_, conn, err := sh.agent.NewPrincipal(sh.ctx, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	ctx, cancel := context.WithCancel(sh.ctx)
	defer cancel()
	if ctx, err = v23.SetNewStreamManager(ctx); err != nil {
		return nil, err
	}
	syscall.ForkLock.RLock()
	fd, err := syscall.Dup(int(conn.Fd()))
	if err != nil {
		syscall.ForkLock.RUnlock()
		return nil, err
	}
	syscall.CloseOnExec(fd)
	syscall.ForkLock.RUnlock()
	p, err := agent.NewAgentPrincipal(ctx, fd, v23.GetClient(ctx))
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}

	// Bless the child principal with blessings derived from the default blessings
	// of shell's principal.
	blessings := sh.principal.BlessingStore().Default()
	blessingForChild, err := sh.principal.Bless(p.PublicKey(), blessings, childBlessingExtension, security.UnconstrainedUse())
	if err != nil {
		return nil, err
	}
	if err := p.BlessingStore().SetDefault(blessingForChild); err != nil {
		return nil, err
	}
	if _, err := p.BlessingStore().Set(blessingForChild, security.AllPrincipals); err != nil {
		return nil, err
	}
	if err := p.AddToRoots(blessingForChild); err != nil {
		return nil, err
	}

	return conn, nil
}

type Main func(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error

// String returns a string representation of the Shell, which is a
// list of the commands currently available in the shell.
func (sh *Shell) String() string {
	return registry.help("")
}

// Help returns the help message for the specified command.
func (sh *Shell) Help(command string) string {
	return registry.help(command)
}

// Start starts the specified command, it returns a Handle which can be
// used for interacting with that command.
//
// The environment variables for the command are set by merging variables
// from the OS environment, those in this Shell and those provided as a
// parameter to it. In general, it prefers values from its parameter over
// those from the Shell, over those from the OS. However, the VeyronCredentials
// and agent FdEnvVar variables will never use the value from the Shell or OS.
//
// If the shell is managing principals, the command is configured to
// connect to the shell's agent.
// To override this, or if the shell is not managing principals, set
// the VeyronCredentials environment variable in the 'env' parameter.
//
// The Shell tracks all of the Handles that it creates so that it can shut
// them down when asked to. The returned Handle may be non-nil even when an
// error is returned, in which case it may be used to retrieve any output
// from the failed command.
//
// Commands must have already been registered using RegisterFunction
// or RegisterChild.
func (sh *Shell) Start(name string, env []string, args ...string) (Handle, error) {
	cmd := registry.getCommand(name)
	if cmd == nil {
		return nil, fmt.Errorf("%s: not registered", name)
	}
	expanded := append([]string{name}, sh.expand(args...)...)
	c := cmd.factory()
	h, err := sh.startCommand(c, env, expanded...)
	if err != nil {
		// If the error is a timeout, then h can be used to recover
		// any output from the process.
		return h, err
	}

	if err := h.WaitForReady(sh.waitTimeout); err != nil {
		return h, err
	}
	return h, nil
}

// StartExternalCommand starts the specified external, non-Vanadium, command;
// it returns a Handle which can be used for interacting with that command.
// A non-Vanadium command does not implement the parent-child protocol
// implemented by the veyron/lib/exec library, thus this method can be used
// to start any command (e.g. /bin/cp).
//
// StartExternalCommand takes an io.Reader which, if non-nil, will be used as
// the stdin for the child process. If this parameter is supplied, then the
// Stdin() method on the returned Handle will return nil. The client of this
// API maintains ownership of stdin and must close it, i.e. the shell
// will not do so.
// The env and args parameters are handled in the same way as Start.
func (sh *Shell) StartExternalCommand(stdin io.Reader, env []string, args ...string) (Handle, error) {
	if len(args) == 0 {
		return nil, errors.New("no arguments specified to StartExternalCommand")
	}
	c := newExecHandleForExternalCommand(args[0], stdin)
	expanded := sh.expand(args...)
	return sh.startCommand(c, env, expanded...)
}

func (sh *Shell) startCommand(c command, env []string, args ...string) (Handle, error) {
	cenv, err := sh.setupCommandEnv(env)
	if err != nil {
		return nil, err
	}
	p, err := sh.NewChildCredentials()
	if err != nil {
		return nil, err
	}

	h, err := c.start(sh, p, cenv, args...)
	if err != nil {
		return nil, err
	}
	sh.mu.Lock()
	sh.handles[h] = struct{}{}
	sh.mu.Unlock()
	return h, nil
}

// SetStartTimeout sets the timeout for starting subcommands.
func (sh *Shell) SetStartTimeout(d time.Duration) {
	sh.startTimeout = d
}

// SetWaitTimeout sets the timeout for waiting on subcommands to complete.
func (sh *Shell) SetWaitTimeout(d time.Duration) {
	sh.waitTimeout = d
}

// CommandEnvelope returns the command line and environment that would be
// used for running the subprocess or function if it were started with the
// specifed arguments.
func (sh *Shell) CommandEnvelope(name string, env []string, args ...string) ([]string, []string) {
	cmd := registry.getCommand(name)
	if cmd == nil {
		return []string{}, []string{}
	}
	menv, err := sh.setupCommandEnv(env)
	if err != nil {
		return []string{}, []string{}
	}
	return cmd.factory().envelope(sh, menv, args...)
}

// Forget tells the Shell to stop tracking the supplied Handle. This is
// generally used when the application wants to control the order that
// commands are shutdown in.
func (sh *Shell) Forget(h Handle) {
	sh.mu.Lock()
	delete(sh.handles, h)
	sh.mu.Unlock()
}

func (sh *Shell) expand(args ...string) []string {
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
	defer sh.mu.Unlock()
	v, present := sh.env[key]
	return v, present
}

// SetVar sets the value to be associated with key.
func (sh *Shell) SetVar(key, value string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	// TODO(cnicolaou): expand value
	sh.env[key] = value
}

// ClearVar removes the speficied variable from the Shell's environment
func (sh *Shell) ClearVar(key string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	delete(sh.env, key)
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
	vars := []string{}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	for k, v := range sh.env {
		vars = append(vars, k+"="+v)
	}
	return vars
}

// Cleanup calls Shutdown on all of the Handles currently being tracked
// by the Shell and writes to stdout and stderr as per the Shutdown
// method in the Handle interface. Cleanup returns the error from the
// last Shutdown that returned a non-nil error. The order that the
// Shutdown routines are executed is not defined.
func (sh *Shell) Cleanup(stdout, stderr io.Writer) error {
	sh.mu.Lock()
	handles := make(map[Handle]struct{})
	for k, v := range sh.handles {
		handles[k] = v
	}
	sh.handles = make(map[Handle]struct{})
	sh.mu.Unlock()
	var err error
	for k, _ := range handles {
		cerr := k.Shutdown(stdout, stderr)
		if cerr != nil {
			err = cerr
		}
	}

	if sh.cancelCtx != nil {
		// Note(ribrdb, caprita): This will shutdown the agents.  If there
		// were errors shutting down it is possible there could be child
		// processes still running, and stopping the agent may cause
		// additional failures.
		sh.cancelCtx()
	}
	os.RemoveAll(sh.tempCredDir)
	return err
}

func (sh *Shell) setupCommandEnv(env []string) ([]string, error) {
	osmap := envSliceToMap(os.Environ())
	evmap := envSliceToMap(env)

	sh.mu.Lock()
	defer sh.mu.Unlock()
	m1 := mergeMaps(osmap, sh.env)
	// Clear any VeyronCredentials directory in m1 as we never
	// want the child to directly use the directory specified
	// by the shell's VeyronCredentials.
	delete(m1, consts.VeyronCredentials)
	delete(m1, agent.FdVarName)

	m2 := mergeMaps(m1, evmap)
	r := []string{}
	for k, v := range m2 {
		r = append(r, k+"="+v)
	}
	return r, nil
}

// Handle represents a running command.
type Handle interface {
	// Stdout returns a reader to the running command's stdout stream.
	Stdout() io.Reader

	// Stderr returns a reader to the running command's stderr
	// stream.
	Stderr() io.Reader

	// Stdin returns a writer to the running command's stdin. The
	// convention is for commands to wait for stdin to be closed before
	// they exit, thus the caller should close stdin when it wants the
	// command to exit cleanly.
	Stdin() io.Writer

	// CloseStdin closes stdin in a manner that avoids a data race
	// between any current readers on it.
	CloseStdin()

	// Shutdown closes the Stdin for the command and then reads output
	// from the command's stdout until it encounters EOF, waits for
	// the command to complete and then reads all of its stderr output.
	// The stdout and stderr contents are written to the corresponding
	// io.Writers if they are non-nil, otherwise the content is discarded.
	Shutdown(stdout, stderr io.Writer) error

	// Pid returns the pid of the process running the command
	Pid() int

	// WaitForReady waits until the child process signals to us that it is
	// ready. If this does not occur within the given timeout duration, a
	// timeout error is returned.
	WaitForReady(timeout time.Duration) error
}

// command is used to abstract the implementations of inprocess and subprocess
// commands.
type command interface {
	envelope(sh *Shell, env []string, args ...string) ([]string, []string)
	start(sh *Shell, agent *os.File, env []string, args ...string) (Handle, error)
}
