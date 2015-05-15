// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"v.io/x/lib/envvar"
	"v.io/x/lib/vlog"

	vexec "v.io/x/ref/lib/exec"
)

type commandDesc struct {
	factory func() command
	main    Main
	help    string
}

type cmdRegistry struct {
	sync.Mutex
	cmds map[string]*commandDesc
}

var registry = &cmdRegistry{cmds: make(map[string]*commandDesc)}

func (r *cmdRegistry) addCommand(name, help string, factory func() command, main Main) {
	r.Lock()
	defer r.Unlock()
	r.cmds[name] = &commandDesc{factory, main, help}
}

func (r *cmdRegistry) getCommand(name string) *commandDesc {
	r.Lock()
	defer r.Unlock()
	return r.cmds[name]
}

func (r *cmdRegistry) getExternalCommand(name string) *commandDesc {
	h := newExecHandleForExternalCommand(name)
	return &commandDesc{
		factory: func() command { return h },
	}
}

// RegisterChild adds a new command to the registry that will be run
// as a subprocess. It must be called before Dispatch or DispatchInTest is
// called, typically from an init function.
func RegisterChild(name, help string, main Main) {
	factory := func() command { return newExecHandle(name) }
	registry.addCommand(name, help, factory, main)
}

// RegisterFunction adds a new command to the registry that will be run
// within the current process. It can be called at any time prior to an
// attempt to use it.
func RegisterFunction(name, help string, main Main) {
	factory := func() command { return newFunctionHandle(name, main) }
	registry.addCommand(name, help, factory, main)
}

// Help returns the help message for the specified command, or a list
// of all commands if the command parameter is an empty string.
func Help(command string) string {
	return registry.help(command)
}

func (r *cmdRegistry) help(command string) string {
	r.Lock()
	defer r.Unlock()
	if len(command) == 0 {
		h := ""
		for c, _ := range r.cmds {
			h += c + ", "
		}
		return strings.TrimRight(h, ", ")
	}
	if c := r.cmds[command]; c != nil {
		return command + ": " + c.help
	}
	return ""
}

const shellEntryPoint = "V23_SHELL_HELPER_PROCESS_ENTRY_POINT"

// IsModulesChildProcess returns true if this process was started by
// the modules package.
func IsModulesChildProcess() bool {
	return os.Getenv(shellEntryPoint) != ""
}

// Dispatch will execute the requested subprocess command from a within a
// a subprocess. It will return without an error if it is executed by a
// process that does not specify an entry point in its environment.
//
// func main() {
//     if modules.IsModulesChildProcess() {
//         if err := modules.Dispatch(); err != nil {
//             panic("error")
//         }
//         eturn
//     }
//     parent code...
//
func Dispatch() error {
	if !IsModulesChildProcess() {
		return nil
	}
	return registry.dispatch()
}

// DispatchAndExit is like Dispatch except that it will call os.Exit(0)
// when executed within a child process and the command succeeds, or panic
// on encountering an error.
//
// func main() {
//     modules.DispatchAndExit()
//     parent code...
//
func DispatchAndExit() {
	if !IsModulesChildProcess() {
		return
	}
	if err := registry.dispatch(); err != nil {
		panic(fmt.Sprintf("unexpected error: %s", err))
	}
	os.Exit(0)
}

func (r *cmdRegistry) dispatch() error {
	ch, err := vexec.GetChildHandle()
	if err != nil {
		// This is for debugging only. It's perfectly reasonable for this
		// error to occur if the process is started by a means other
		// than the exec library.
		vlog.VI(1).Infof("failed to get child handle: %s", err)
	}

	// Only signal that the child is ready or failed if we successfully get
	// a child handle. We most likely failed to get a child handle
	// because the subprocess was run directly from the command line.
	command := os.Getenv(shellEntryPoint)
	if len(command) == 0 {
		err := fmt.Errorf("Failed to find entrypoint %q", command)
		if ch != nil {
			ch.SetFailed(err)
		}
		return err
	}

	m := registry.getCommand(command)
	if m == nil {
		err := fmt.Errorf("%s: not registered", command)
		if ch != nil {
			ch.SetFailed(err)
		}
		return err
	}

	if ch != nil {
		ch.SetReady()
	}

	go func(pid int) {
		for {
			_, err := os.FindProcess(pid)
			if err != nil {
				vlog.Fatalf("Looks like our parent exited: %v", err)
			}
			time.Sleep(time.Second)
		}
	}(os.Getppid())

	flag.Parse()
	return m.main(os.Stdin, os.Stdout, os.Stderr, envvar.SliceToMap(os.Environ()), flag.Args()...)
}

// WaitForEOF returns when a read on its io.Reader parameter returns io.EOF
func WaitForEOF(stdin io.Reader) {
	buf := [1024]byte{}
	for {
		if _, err := stdin.Read(buf[:]); err == io.EOF {
			return
		}
	}
}
