// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
)

func simpleEchoProgram(stdin io.Reader, stdout io.Writer) {
	fmt.Fprintf(stdout, "ready\n")
	scanner := bufio.NewScanner(stdin)
	if scanner.Scan() {
		fmt.Fprintf(stdout, "%s\n", scanner.Text())
	}
	modules.WaitForEOF(stdin)
}

var withRuntime = modules.Register(func(env *modules.Env, args ...string) error {
	_, shutdown := test.InitForTest()
	defer shutdown()

	simpleEchoProgram(env.Stdin, env.Stdout)
	return nil
}, "withRuntime")

var withoutRuntime = modules.Register(func(env *modules.Env, args ...string) error {
	simpleEchoProgram(env.Stdin, env.Stdout)
	return nil
}, "withoutRuntime")

func TestWithRuntime(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start(nil, withRuntime)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer h.Shutdown(os.Stderr, os.Stderr)
	h.Expect("ready")
	syscall.Kill(h.Pid(), syscall.SIGHUP)
	h.Stdin().Write([]byte("foo\n"))
	h.Expect("foo")
	h.CloseStdin()
	h.ExpectEOF()
}

func TestWithoutRuntime(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	opts := sh.DefaultStartOpts()
	opts.ShutdownTimeout = 5 * time.Second
	h, err := sh.StartWithOpts(opts, nil, withoutRuntime)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer h.Shutdown(os.Stderr, os.Stderr)
	h.Expect("ready")
	syscall.Kill(h.Pid(), syscall.SIGHUP)
	h.ExpectEOF()
	err = h.Shutdown(os.Stderr, os.Stderr)
	want := "exit status 2"
	if err == nil || err.Error() != want {
		t.Errorf("got %s, want %s", err, want)

	}
}
