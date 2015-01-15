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
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"sync"
	"syscall"
	"time"

	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/security/agent"
	"v.io/core/veyron/security/agent/keymgr"
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
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
	blessing                  security.Blessings
	agent                     *keymgr.Agent
	ctx                       *context.T
	cancelCtx                 func()
}

// NewShell creates a new instance of Shell.
// If ctx is non-nil, the shell will manage Principals for child processes.
// By default it adds a blessing to ctx's principal to ensure isolation. Any child processes
// are created with principals derived from this new blessing.
// However, if p is non-nil the shell will skip the new blessing and child processes
// will have their principals derived from p's default blessing(s).
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
	if ctx, _, err = veyron2.SetNewStreamManager(ctx); err != nil {
		return nil, err
	}
	sh.ctx = ctx

	if sh.tempCredDir, err = ioutil.TempDir("", "shell_credentials"); err != nil {
		return nil, err
	}
	if sh.agent, err = keymgr.NewLocalAgent(ctx, sh.tempCredDir, nil); err != nil {
		return nil, err
	}
	if p != nil {
		sh.principal = p
		sh.blessing = p.BlessingStore().Default()
		return sh, nil
	}
	p = veyron2.GetPrincipal(ctx)
	sh.principal = p
	gen := rand.New(rand.NewSource(time.Now().UnixNano()))
	// Use a unique blessing tree per shell.
	blessingName := fmt.Sprintf("%s-%d", shellBlessingExtension, gen.Int63())
	blessing, err := p.BlessSelf(blessingName)
	if err != nil {
		return nil, err
	}
	sh.blessing = blessing
	if _, err := p.BlessingStore().Set(blessing, security.BlessingPattern(blessingName+"/"+childBlessingExtension+"/...")); err != nil {
		return nil, err
	}
	if err := p.AddToRoots(blessing); err != nil {
		return nil, err
	}
	// Our blessing store now contains a blessing with our unique prefix
	// and the principal has that blessing's root added to its trusted
	// list so that it will accept blessings derived from it.
	return sh, nil
}

func (sh *Shell) getChildCredentials() (*os.File, error) {
	if sh.ctx == nil {
		return nil, nil
	}
	root := sh.principal
	rootBlessing := sh.blessing
	_, conn, err := sh.agent.NewPrincipal(sh.ctx, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(sh.ctx)
	if ctx, _, err = veyron2.SetNewStreamManager(ctx); err != nil {
		return nil, err
	}
	defer cancel()
	fd, err := syscall.Dup(int(conn.Fd()))
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	p, err := agent.NewAgentPrincipal(ctx, fd)
	if err != nil {
		return nil, err
	}
	blessingForChild, err := root.Bless(p.PublicKey(), rootBlessing, childBlessingExtension, security.UnconstrainedUse())
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

	if sh.blessing != nil {
		// Create a second blessing for the child, that will be accepted
		// by the parent, should the child choose to invoke RPCs on the parent.
		blessingFromChild, err := root.Bless(p.PublicKey(), root.BlessingStore().Default(), childBlessingExtension, security.UnconstrainedUse())
		if err != nil {
			return nil, err
		}
		// We store this blessing as the one to use with a pattern that matches
		// the root's name.
		// TODO(cnicolaou,caprita): at some point there will be a nicer API
		// for getting the name of a blessing.
		ctx := security.NewContext(&security.ContextParams{LocalPrincipal: root})
		rootName := root.BlessingStore().Default().ForContext(ctx)[0]
		if _, err := p.BlessingStore().Set(blessingFromChild, security.BlessingPattern(rootName)); err != nil {
			return nil, err
		}

		if err := p.AddToRoots(blessingFromChild); err != nil {
			return nil, err
		}
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
// environment variable is handled specially.
//
// If the VeyronCredentials environment variable is set in 'env' then that
// is the value that gets used. If the shell's VeyronCredentials are set then
// VeyronCredentials for the command are set to a freshly created directory
// specifying a principal blessed by the shell's credentials. In all other
// cases VeyronCredentials for the command remains unset.
//
// The Shell tracks all of the Handles that it creates so that it can shut
// them down when asked to. The returned Handle may be non-nil even when an
// error is returned, in which case it may be used to retrieve any output
// from the failed command.
//
// Commands must have already been registered using RegisterFunction
// or RegisterChild.
func (sh *Shell) Start(name string, env []string, args ...string) (Handle, error) {
	cenv, err := sh.setupCommandEnv(env)
	if err != nil {
		return nil, err
	}
	p, err := sh.getChildCredentials()
	if err != nil {
		return nil, err
	}
	cmd := registry.getCommand(name)
	if cmd == nil {
		return nil, fmt.Errorf("%s: not registered", name)
	}
	expanded := append([]string{name}, sh.expand(args...)...)
	h, err := cmd.factory().start(sh, p, cenv, expanded...)
	if err != nil {
		// If the error is a timeout, then h can be used to recover
		// any output from the process.
		return h, err
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
//
// This method is not idempotent as the directory pointed to by the
// VeyronCredentials environment variable may be freshly created with
// each invocation.
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
//
// Note that setting the VeyronCredentials environement
// variable changes the shell's principal which is used
// for blessing the principals supplied to the shell's
// children.
func (sh *Shell) SetVar(key, value string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	// TODO(cnicolaou): expand value
	sh.env[key] = value
}

// ClearVar removes the speficied variable from the Shell's environment
//
// Note that clearing the VeyronCredentials environment variable
// would amount to clearing the shell's principal, and therefore, any
// children of this shell would have their VeyronCredentials environment
// variable set only if it is explicitly set in their parameters.
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
}

// command is used to abstract the implementations of inprocess and subprocess
// commands.
type command interface {
	envelope(sh *Shell, env []string, args ...string) ([]string, []string)
	start(sh *Shell, agent *os.File, env []string, args ...string) (Handle, error)
}
