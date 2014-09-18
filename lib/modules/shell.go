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
package modules

import (
	"fmt"
	"io"
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
}

type commandDesc struct {
	factory func() command
	help    string
}

type childRegistrar struct {
	sync.Mutex
	mains map[string]Main
}

var child = &childRegistrar{mains: make(map[string]Main)}

// NewShell creates a new instance of Shell.
func NewShell() *Shell {
	// TODO(cnicolaou): should create a new identity if one doesn't
	// already exist
	return &Shell{
		env:     make(map[string]string),
		cmds:    make(map[string]*commandDesc),
		handles: make(map[Handle]struct{}),
	}
}

type Main func(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error

// AddSubprocess adds a new command to the Shell that will be run
// as a subprocess. In addition, the child process must call RegisterChild
// using the same name used here and provide the function to be executed
// in the child.
func (sh *Shell) AddSubprocess(name string, help string) {
	if !child.hasCommand(name) {
		vlog.Infof("Warning: %q is not registered with modules.Dispatcher", name)
	}
	entryPoint := shellEntryPoint + "=" + name
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
func (sh *Shell) Start(command string, args ...string) (Handle, error) {
	sh.mu.Lock()
	cmd := sh.cmds[command]
	if cmd == nil {
		sh.mu.Unlock()
		return nil, fmt.Errorf("command %q is not available", command)
	}
	expanded := sh.expand(args...)
	sh.mu.Unlock()
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
// by the Shell. Any buffered output from the command's stderr stream
// will be written to the supplied io.Writer. If the io.Writer is nil
// then any such output is lost.
func (sh *Shell) Cleanup(output io.Writer) {
	sh.mu.Lock()
	handles := make(map[Handle]struct{})
	for k, v := range sh.handles {
		handles[k] = v
	}
	sh.handles = make(map[Handle]struct{})
	sh.mu.Unlock()
	for k, _ := range handles {
		k.Shutdown(output)
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
	// from the command's stdout until it encounters EOF and writes that
	// output to the supplied io.Writer. It returns any error returned by
	// the command.
	Shutdown(io.Writer) error
}

// command is used to abstract the implementations of inprocess and subprocess
// commands.
type command interface {
	start(sh *Shell, args ...string) (Handle, error)
}
