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
// Every Shell created by NewShell is initialized with its VeyronCredentials
// environment variable set. The variable is set to the os's VeyronCredentials
// if that is set, otherwise to a freshly created credentials directory. The
// shell's VeyronCredentials can be set and cleared using the SetVar and
// ClearVar methods respectively.
//
// By default, the VeyronCredentials for each command Start-ed by the shell are
// set to a freshly created credentials directory that is blessed by the shell's
// credentials (i.e., if shell's credentials have not been cleared). Thus, each
// child of the shell gets its own credentials directory with a blessing from the
// shell of the form
//   <shell's default blessing>/child
// These default credentials provided by the shell to each command can be
// overridden by specifying VeyronCredentials in the environment provided as a
// parameter to the Start method.
package modules

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/lib/flags/consts"
	vsecurity "veyron.io/veyron/veyron/security"

	"veyron.io/veyron/veyron2/security"
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
	// tmpCredDirs are the temporary directories created by this
	// shell. These must be removed when the shell is cleaned up.
	tempCredDirs              []string
	startTimeout, waitTimeout time.Duration
	config                    exec.Config
}

// NewShell creates a new instance of Shell.
//
// The VeyronCredentials environment variable of the shell is set
// as follows. If the OS's VeyronCredentials is set then the shell's
// VeyronCredentials is set to the same value, otherwise it is set
// to a freshly created credentials directory.
//
// The shell's credentials are used to bless the principal supplied
// to any children of this shell (see Start).
// TODO(cnicolaou): this should use the principal already setup
// with the runtime if the runtime has been initialized, if not,
// it should create a new principal. As of now, this approach only works
// for child processes that talk to each other, but not to the parent
// process that started them since it's running with a different set of
// credentials setup elsewhere. When this change is made it should
// be possible to remove creating credentials in many unit tests.
func NewShell() *Shell {
	sh := &Shell{
		env:          make(map[string]string),
		handles:      make(map[Handle]struct{}),
		startTimeout: time.Minute,
		waitTimeout:  10 * time.Second,
		config:       exec.NewConfig(),
	}
	if err := sh.initShellCredentials(); err != nil {
		// TODO(cnicolaou): return an error rather than panic.
		panic(err)
	}
	return sh
}

func (sh *Shell) initShellCredentials() error {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if dir := os.Getenv(consts.VeyronCredentials); len(dir) != 0 {
		sh.env[consts.VeyronCredentials] = dir
		return nil
	}

	dir, err := ioutil.TempDir("", "shell_credentials")
	if err != nil {
		return err
	}
	sh.env[consts.VeyronCredentials] = dir
	sh.tempCredDirs = append(sh.tempCredDirs, dir)
	return nil
}

func (sh *Shell) getChildCredentials(shellCredDir string) (string, error) {
	root, err := principalFromDir(shellCredDir)
	if err != nil {
		return "", err
	}

	dir, err := ioutil.TempDir("", "shell_child_credentials")
	if err != nil {
		return "", err
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, nil)
	if err != nil {
		return "", err
	}

	blessing, err := root.Bless(p.PublicKey(), root.BlessingStore().Default(), childBlessingExtension, security.UnconstrainedUse())
	if err != nil {
		return "", err
	}
	if err := p.BlessingStore().SetDefault(blessing); err != nil {
		return "", err
	}
	if _, err := p.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
		return "", err
	}
	if err := p.AddToRoots(blessing); err != nil {
		return "", err
	}

	sh.tempCredDirs = append(sh.tempCredDirs, dir)
	return dir, nil
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
	cmd := registry.getCommand(name)
	if cmd == nil {
		return nil, fmt.Errorf("%s: not registered", name)
	}
	expanded := append([]string{name}, sh.expand(args...)...)
	h, err := cmd.factory().start(sh, cenv, expanded...)
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

	// TODO(ataly, ashankar, caprita): The following code may lead to
	// removing the credential directories for child processes that are
	// still alive (this can happen e.g. if the Wait on the child timed
	// out). While we can hope that some error will reach the caller of
	// Cleanup (because we harvest the error codes from Shutdown), it may
	// lead to failures that are harder to debug because the original issue
	// will get masked by the credentials going away. One possibility is
	// to only remove the credentials dir when Shutdown returns no timeout
	// error.
	for _, dir := range sh.tempCredDirs {
		os.RemoveAll(dir)
	}
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

	// Set the VeyronCredentials environment variable for the child
	// if it is not already set and the shell's VeyronCredentials are set.
	if len(evmap[consts.VeyronCredentials]) == 0 && len(sh.env[consts.VeyronCredentials]) != 0 {
		var err error
		if evmap[consts.VeyronCredentials], err = sh.getChildCredentials(sh.env[consts.VeyronCredentials]); err != nil {
			return nil, err
		}
	}

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
	start(sh *Shell, env []string, args ...string) (Handle, error)
}
