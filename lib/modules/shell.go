// Package modules provides a mechanism for running commonly used services
// as subprocesses and client functionality for accessing those services.
// Such services and functions are collectively called 'commands' and are
// registered with and executed within a context, defined by the Shell type.
// The Shell is analagous to the original UNIX shell and maintains a
// key, value store of variables that is accessible to all of the commands that
// it hosts. These variables may be referenced by the arguments passed to
// commands.
//
// Commands are added to a shell in two ways: one for a subprocess and another
// for an inprocess function.
//
// - subprocesses are added using the AddSubprocess method in the parent
//   and by the modules.RegisterChild function in the child process (typically
//   RegisterChild is called from an init function). modules.Dispatch must
//   be called in the child process to execute the subprocess 'Main' function
//   provided to RegisterChild.
// - inprocess functions are added using the AddFunction method.
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
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"veyron.io/veyron/veyron2/vlog"
)

// Shell represents the context within which commands are run.
type Shell struct {
	mu      sync.Mutex
	env     map[string]string
	cmds    map[string]*commandDesc
	handles map[Handle]struct{}
	credDir string
}

type commandDesc struct {
	factory func() command
	help    string
}

type childEntryPoint struct {
	fn   Main
	help string
}

type childRegistrar struct {
	sync.Mutex
	mains map[string]*childEntryPoint
}

var child = &childRegistrar{mains: make(map[string]*childEntryPoint)}

// NewShell creates a new instance of Shell. If this new instance is
// is a test and no credentials have been configured in the environment
// via VEYRON_CREDENTIALS then CreateAndUseNewCredentials will be used to
// configure a new ID for the shell and its children.
// NewShell takes optional regexp patterns that can be used to specify
// subprocess commands that are implemented in the same binary as this shell
// (i.e. have been registered using modules.RegisterChild) to be
// automatically added to it. If the patterns fail to match any such command
// then they have no effect.
func NewShell(patterns ...string) *Shell {
	// TODO(cnicolaou): should create a new identity if one doesn't
	// already exist
	sh := &Shell{
		env:     make(map[string]string),
		cmds:    make(map[string]*commandDesc),
		handles: make(map[Handle]struct{}),
	}
	if flag.Lookup("test.run") != nil && os.Getenv("VEYRON_CREDENTIALS") == "" {
		if err := sh.CreateAndUseNewCredentials(); err != nil {
			// TODO(cnicolaou): return an error rather than panic.
			panic(err)
		}
	}
	for _, pattern := range patterns {
		child.addSubprocesses(sh, pattern)
	}
	return sh
}

// CreateAndUseNewCredentials setups a new credentials directory and then
// configures the shell and all of its children to use to it.
func (sh *Shell) CreateAndUseNewCredentials() error {
	dir, err := ioutil.TempDir("", "veyron_credentials")
	if err != nil {
		return err
	}
	sh.credDir = dir
	sh.SetVar("VEYRON_CREDENTIALS", sh.credDir)
	return nil
}

type Main func(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error

// AddSubprocess adds a new command to the Shell that will be run
// as a subprocess. In addition, the child process must call RegisterChild
// using the same name used here and provide the function to be executed
// in the child.
func (sh *Shell) AddSubprocess(name, help string) {
	if !child.hasCommand(name) {
		vlog.Infof("Warning: %q is not registered with modules.Dispatcher", name)
	}
	sh.addSubprocess(name, help)
}

func (sh *Shell) addSubprocess(name string, help string) {
	entryPoint := ShellEntryPoint + "=" + name
	sh.mu.Lock()
	sh.cmds[name] = &commandDesc{func() command { return newExecHandle(entryPoint) }, help}
	sh.mu.Unlock()
}

// AddFunction adds a new command to the Shell that will be run
// within the current process.
func (sh *Shell) AddFunction(name string, main Main, help string) {
	sh.mu.Lock()
	sh.cmds[name] = &commandDesc{func() command { return newFunctionHandle(main) }, help}
	sh.mu.Unlock()
}

// String returns a string representation of the Shell, which is a
// list of the commands currently available in the shell.
func (sh *Shell) String() string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	h := ""
	for n, _ := range sh.cmds {
		h += n + ", "
	}
	return strings.TrimRight(h, ", ")
}

// Help returns the help message for the specified command.
func (sh *Shell) Help(command string) string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if c := sh.cmds[command]; c != nil {
		return command + ": " + c.help
	}
	return ""
}

// Start starts the specified command, it returns a Handle which can be used
// for interacting with that command. The Shell tracks all of the Handles
// that it creates so that it can shut them down when asked to.
// Commands may have already been registered with the Shell using AddFunction
// or AddSubprocess, but if not, they will treated as subprocess commands
// and an attempt made to run them. Such 'dynamically' started subprocess
// commands are not remembered the by Shell and do not provide a 'help'
// message etc; their handles are remembered and will be acted on by
// the Cleanup method. If the non-registered subprocess command does not
// exist then the Start command will return an error.
func (sh *Shell) Start(name string, args ...string) (Handle, error) {
	sh.mu.Lock()
	cmd := sh.cmds[name]
	sh.mu.Unlock()
	if cmd == nil {
		entryPoint := ShellEntryPoint + "=" + name
		cmd = &commandDesc{func() command { return newExecHandle(entryPoint) }, ""}
	}
	expanded := append([]string{name}, sh.expand(args...)...)
	h, err := cmd.factory().start(sh, expanded...)
	if err != nil {
		return nil, err
	}
	sh.mu.Lock()
	sh.handles[h] = struct{}{}
	sh.mu.Unlock()
	return h, nil
}

// forget tells the Shell to stop tracking the supplied Handle.
func (sh *Shell) forget(h Handle) {
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
// method in the Handle interface.
func (sh *Shell) Cleanup(stdout, stderr io.Writer) {
	sh.mu.Lock()
	handles := make(map[Handle]struct{})
	for k, v := range sh.handles {
		handles[k] = v
	}
	sh.handles = make(map[Handle]struct{})
	sh.mu.Unlock()
	for k, _ := range handles {
		k.Shutdown(stdout, stderr)
	}
	if len(sh.credDir) > 0 {
		os.RemoveAll(sh.credDir)
	}
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
	start(sh *Shell, args ...string) (Handle, error)
}
