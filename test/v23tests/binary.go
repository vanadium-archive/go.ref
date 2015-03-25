// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v23tests

import (
	"bytes"
	"io"
	"os"
	"path"
	"strings"

	"v.io/x/lib/vlog"

	"v.io/x/ref/test/modules"
)

// Binary represents an executable program that will be executed during a
// test. A binary may be invoked multiple times by calling Start, each call
// will return a new Invocation.
//
// Binary instances are typically obtained from a T by calling BuldV23Pkg,
// BuildGoPkg (for Vanadium and other Go binaries) or BinaryFromPath (to
// start binaries that are already present on the system).
type Binary struct {
	// The environment to which this binary belongs.
	env *T

	// The path to the binary.
	path string

	// StartOpts
	opts modules.StartOpts

	// Environment variables that will be used when creating invocations
	// via Start.
	envVars []string
}

func (b *Binary) cleanup() {
	binaryDir := path.Dir(b.path)
	vlog.Infof("cleaning up %s", binaryDir)
	if err := os.RemoveAll(binaryDir); err != nil {
		vlog.Infof("WARNING: RemoveAll(%s) failed (%v)", binaryDir, err)
	}
}

// StartOpts returns the current the StartOpts
func (b *Binary) StartOpts() modules.StartOpts {
	return b.opts
}

// Path returns the path to the binary.
func (b *Binary) Path() string {
	return b.path
}

// Start starts the given binary with the given arguments.
func (b *Binary) Start(args ...string) *Invocation {
	return b.start(1, args...)
}

func (b *Binary) start(skip int, args ...string) *Invocation {
	vlog.Infof("%s: starting %s %s", Caller(skip+1), b.Path(), strings.Join(args, " "))
	opts := b.opts
	if opts.ExecProtocol && opts.Credentials == nil {
		opts.Credentials, opts.Error = b.env.shell.NewChildCredentials("child")
	}
	opts.ExpectTesting = b.env.TB
	handle, err := b.env.shell.StartWithOpts(opts, b.envVars, b.Path(), args...)
	if err != nil {
		if handle != nil {
			vlog.Infof("%s: start failed", Caller(skip+1))
			handle.Shutdown(nil, os.Stderr)
		}
		// TODO(cnicolaou): calling Fatalf etc from a goroutine often leads
		// to deadlock. Need to make sure that we handle this here. Maybe
		// it's best to just return an error? Or provide a StartWithError
		// call for use from goroutines.
		b.env.Fatalf("%s: StartWithOpts(%v, %v) failed: %v", Caller(skip+1), b.Path(), strings.Join(args, ", "), err)
	}
	vlog.Infof("started PID %d\n", handle.Pid())
	inv := &Invocation{
		env:         b.env,
		path:        b.path,
		args:        args,
		shutdownErr: errNotShutdown,
		Handle:      handle,
	}
	b.env.appendInvocation(inv)
	return inv
}

func (b *Binary) run(args ...string) string {
	inv := b.start(2, args...)
	var stdout, stderr bytes.Buffer
	err := inv.Wait(&stdout, &stderr)
	if err != nil {
		a := strings.Join(args, ", ")
		b.env.Fatalf("%s: Run(%s): failed: %v: \n%s\n", Caller(2), a, err, stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\n")
}

// Run runs the binary with the specified arguments to completion. On
// success it returns the contents of stdout, on failure it terminates the
// test with an error message containing the error and the contents of
// stderr.
func (b *Binary) Run(args ...string) string {
	return b.run(args...)
}

// WithStdin returns a copy of this binary that, when Start is called,
// will read its input from the given reader. Once the reader returns
// EOF, the returned invocation's standard input will be closed (see
// Invocation.CloseStdin).
func (b *Binary) WithStdin(r io.Reader) *Binary {
	opts := b.opts
	opts.Stdin = r
	return b.WithStartOpts(opts)
}

// WithEnv returns a copy of this binary that, when Start is called, will use
// the given environment variables. Each environment variable should be
// in "key=value" form. For example:
//
// bin.WithEnv("EXAMPLE_ENV=/tmp/something").Start(...)
func (b *Binary) WithEnv(env ...string) *Binary {
	newBin := *b
	newBin.envVars = env
	return &newBin
}

// WithStartOpts eturns a copy of this binary that, when Start is called, will
// use the given StartOpts.
//
// bin.WithStartOpts(opts).Start(...)
// or
// bin.WithStartOpts().Run(...)
func (b *Binary) WithStartOpts(opts modules.StartOpts) *Binary {
	newBin := *b
	newBin.opts = opts
	return &newBin
}
