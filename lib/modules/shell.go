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
// If a Shell is created within a unit test then it will automatically
// generate a security ID, write it to a file and set the appropriate
// environment variable to refer to it.
package modules

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/lib/flags/consts"
)

// Shell represents the context within which commands are run.
type Shell struct {
	mu           sync.Mutex
	env          map[string]string
	handles      map[Handle]struct{}
	credDir      string
	startTimeout time.Duration
	config       exec.Config
}

// NewShell creates a new instance of Shell. If this new instance is is a test
// and no credentials have been configured in the environment via
// consts.VeyronCredentials then CreateAndUseNewCredentials will be used to
// configure a new ID for the shell and its children.
func NewShell() *Shell {
	sh := &Shell{
		env:          make(map[string]string),
		handles:      make(map[Handle]struct{}),
		startTimeout: time.Minute,
		config:       exec.NewConfig(),
	}
	if flag.Lookup("test.run") != nil && os.Getenv(consts.VeyronCredentials) == "" {
		if err := sh.CreateAndUseNewCredentials(); err != nil {
			// TODO(cnicolaou): return an error rather than panic.
			panic(err)
		}
	}
	return sh
}

// CreateAndUseNewCredentials setups a new credentials directory and then
// configures the shell and all of its children to use to it.
//
// TODO(cnicolaou): this should use the principal already setup
// with the runtime if the runtime has been initialized, if not,
// it should create a new principal. As of now, this approach only works
// for child processes that talk to each other, but not to the parent
// process that started them since it's running with a different set of
// credentials setup elsewhere. When this change is made it should
// be possible to remove creating credentials in many unit tests.
func (sh *Shell) CreateAndUseNewCredentials() error {
	dir, err := ioutil.TempDir("", "veyron_credentials")
	if err != nil {
		return err
	}
	sh.credDir = dir
	sh.SetVar(consts.VeyronCredentials, sh.credDir)
	return nil
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

// Start starts the specified command, it returns a Handle which can be used
// for interacting with that command. The OS and shell environment variables
// are merged with the ones supplied as a parameter; the parameter specified
// ones override the Shell and the Shell ones override the OS ones.
//
// The Shell tracks all of the Handles that it creates so that it can shut
// them down when asked to. The returned Handle may be non-nil even when an
// error is returned, in which case it may be used to retrieve any output
// from the failed command.
//
// Commands must have already been registered using RegisterFunction
// or RegisterChild.
func (sh *Shell) Start(name string, env []string, args ...string) (Handle, error) {
	cenv := sh.MergedEnv(env)
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

// CommandEnvelope returns the command line and environment that would be
// used for running the subprocess or function if it were started with the
// specifed arguments.
func (sh *Shell) CommandEnvelope(name string, env []string, args ...string) ([]string, []string) {
	cmd := registry.getCommand(name)
	if cmd == nil {
		return []string{}, []string{}
	}
	return cmd.factory().envelope(sh, sh.MergedEnv(env), args...)
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
	if len(sh.credDir) > 0 {
		os.RemoveAll(sh.credDir)
	}
	return err
}

// MergedEnv returns a slice that contains the merged set of environment
// variables from the OS environment, those in this Shell and those provided
// as a parameter to it. It prefers values from its parameter over those
// from the Shell, over those from the OS.
func (sh *Shell) MergedEnv(env []string) []string {
	osmap := envSliceToMap(os.Environ())
	evmap := envSliceToMap(env)
	sh.mu.Lock()
	m1 := mergeMaps(osmap, sh.env)
	defer sh.mu.Unlock()
	m2 := mergeMaps(m1, evmap)
	r := []string{}
	for k, v := range m2 {
		r = append(r, k+"="+v)
	}
	return r
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
