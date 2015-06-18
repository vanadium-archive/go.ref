// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"v.io/x/ref/internal/logger"
	vexec "v.io/x/ref/lib/exec"
)

// Program is a symbolic representation of a registered Main function.
type Program string

type programInfo struct {
	main    Main
	factory func() *execHandle
}

type programRegistry struct {
	programs []*programInfo
}

var registry = new(programRegistry)

func (r *programRegistry) addProgram(main Main, description string) Program {
	prog := strconv.Itoa(len(r.programs))
	factory := func() *execHandle { return newExecHandle(prog, description) }
	r.programs = append(r.programs, &programInfo{main, factory})
	return Program(prog)
}

func (r *programRegistry) getProgram(prog Program) *programInfo {
	index, err := strconv.Atoi(string(prog))
	if err != nil || index < 0 || index >= len(r.programs) {
		return nil
	}
	return r.programs[index]
}

func (r *programRegistry) getExternalProgram(prog Program) *programInfo {
	h := newExecHandleExternal(string(prog))
	return &programInfo{
		factory: func() *execHandle { return h },
	}
}

func (r *programRegistry) String() string {
	var s string
	for _, info := range r.programs {
		h := info.factory()
		s += fmt.Sprintf("%s: %s\n", h.entryPoint, h.desc)
	}
	return s
}

// Register adds a new program to the registry that will be run as a subprocess.
// It must be called before Dispatch is called, typically from an init function.
func Register(main Main, description string) Program {
	if _, file, line, ok := runtime.Caller(1); ok {
		description = fmt.Sprintf("%s:%d %s", shortFile(file), line, description)
	}
	return registry.addProgram(main, description)
}

// shortFile returns the last 3 components of the given file name.
func shortFile(file string) string {
	var short string
	for i := 0; i < 3; i++ {
		short = filepath.Join(filepath.Base(file), short)
		file = filepath.Dir(file)
	}
	return short
}

const shellEntryPoint = "V23_SHELL_HELPER_PROCESS_ENTRY_POINT"

// IsChildProcess returns true if this process was started by the modules
// package.
func IsChildProcess() bool {
	return os.Getenv(shellEntryPoint) != ""
}

// Dispatch executes the requested subprocess program from within a subprocess.
// Returns an error if it is executed by a process that does not specify an
// entry point in its environment.
func Dispatch() error {
	return registry.dispatch()
}

// DispatchAndExitIfChild is a convenience function with three possible results:
//   * os.Exit(0) if called within a child process, and the dispatch succeeds.
//   * os.Exit(1) if called within a child process, and the dispatch fails.
//   * return with no side-effects, if not called within a child process.
func DispatchAndExitIfChild() {
	if IsChildProcess() {
		if err := Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func (r *programRegistry) dispatch() error {
	ch, err := vexec.GetChildHandle()
	if err != nil {
		// This is for debugging only. It's perfectly reasonable for this
		// error to occur if the process is started by a means other
		// than the exec library.
		logger.Global().VI(1).Infof("failed to get child handle: %s", err)
	}

	// Only signal that the child is ready or failed if we successfully get
	// a child handle. We most likely failed to get a child handle
	// because the subprocess was run directly from the command line.
	prog := os.Getenv(shellEntryPoint)
	if prog == "" {
		err := fmt.Errorf("Failed to find entrypoint %q", prog)
		if ch != nil {
			ch.SetFailed(err)
		}
		return err
	}

	m := registry.getProgram(Program(prog))
	if m == nil {
		err := fmt.Errorf("%s: not registered\n%s", prog, registry.String())
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
				logger.Global().Fatalf("Looks like our parent exited: %v", err)
			}
			time.Sleep(time.Second)
		}
	}(os.Getppid())

	flag.Parse()
	return m.main(EnvFromOS(), flag.Args()...)
}

// WaitForEOF returns when a read on its io.Reader parameter returns io.EOF
func WaitForEOF(r io.Reader) {
	io.Copy(ioutil.Discard, r)
}
