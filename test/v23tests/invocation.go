// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v23tests

import (
	"bytes"
	"container/list"
	"io"
	"syscall"

	"v.io/x/lib/vlog"
	"v.io/x/ref/test/modules"
)

// Invocation represents a single invocation of a Binary.
//
// Any bytes written by the invocation to its standard error may be recovered
// using the Wait or WaitOrDie functions.
//
// For example:
//   bin := env.BinaryFromPath("/bin/bash")
//   inv := bin.Start("-c", "echo hello world 1>&2")
//   var stderr bytes.Buffer
//   inv.WaitOrDie(nil, &stderr)
//   // stderr.Bytes() now contains 'hello world\n'
type Invocation struct {
	// The handle to the process that was run when this invocation was started.
	modules.Handle

	// The environment to which this invocation belongs.
	env *T

	// The element representing this invocation in the list of
	// invocations stored in the environment
	el *list.Element

	// The path of the binary used for this invocation.
	path string

	// The args the binary was started with
	args []string

	// True if the process has been shutdown
	hasShutdown bool

	// The error, if any, as determined when the invocation was
	// shutdown. It must be set to a default initial value of
	// errNotShutdown rather than nil to allow us to distinguish between
	// a successful shutdown or an error.
	shutdownErr error
}

// Path returns the path to the binary that was used for this invocation.
func (i *Invocation) Path() string {
	return i.path
}

// Exists returns true if the invocation still exists.
func (i *Invocation) Exists() bool {
	return syscall.Kill(i.Handle.Pid(), 0) == nil
}

// Kill sends the given signal to this invocation. It is up to the test
// author to decide whether failure to deliver the signal is fatal to
// the test.
func (i *Invocation) Kill(sig syscall.Signal) error {
	pid := i.Handle.Pid()
	vlog.VI(1).Infof("sending signal %v to PID %d", sig, pid)
	return syscall.Kill(pid, sig)
}

// Output reads the invocation's stdout until EOF and then returns what
// was read as a string.
func (i *Invocation) Output() string {
	buf := bytes.Buffer{}
	_, err := buf.ReadFrom(i.Stdout())
	if err != nil {
		i.env.Fatalf("%s: ReadFrom() failed: %v", Caller(1), err)
	}
	return buf.String()
}

// Wait waits for this invocation to finish. If either stdout or stderr
// is non-nil, any remaining unread output from those sources will be
// written to the corresponding writer. The returned error represents
// the exit status of the underlying command.
func (i *Invocation) Wait(stdout, stderr io.Writer) error {
	err := i.Handle.Shutdown(stdout, stderr)
	i.hasShutdown = true
	i.shutdownErr = err
	return err
}

// WaitOrDie waits for this invocation to finish. If either stdout or stderr
// is non-nil, any remaining unread output from those sources will be
// written to the corresponding writer. If the underlying command
// exited with anything but success (exit status 0), this function will
// cause the current test to fail.
func (i *Invocation) WaitOrDie(stdout, stderr io.Writer) {
	if err := i.Wait(stdout, stderr); err != nil {
		i.env.Fatalf("%s: FATAL: Wait() for pid %d failed: %v", Caller(1), i.Handle.Pid(), err)
	}
}

// Environment returns the instance of the test environment that this
// invocation was from.
func (i *Invocation) Environment() *T {
	return i.env
}
