// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt_test

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"testing"

	"v.io/x/lib/gosh"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23test"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

func simpleEchoProgram() {
	fmt.Printf("ready\n")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		fmt.Printf("%s\n", scanner.Text())
	}
	<-signals.ShutdownOnSignals(nil)
}

var withRuntime = gosh.Register("withRuntime", func() {
	_, shutdown := test.V23Init()
	defer shutdown()
	simpleEchoProgram()
})

var withoutRuntime = gosh.Register("withoutRuntime", func() {
	simpleEchoProgram()
})

func TestWithRuntime(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{PropagateChildOutput: true})
	defer sh.Cleanup()

	c := sh.Fn(withRuntime)
	stdin := c.StdinPipe()
	c.Start()
	c.S.Expect("ready")
	// The Vanadium runtime spawns a goroutine that listens for SIGHUP and
	// prevents process exit.
	c.Signal(syscall.SIGHUP)
	stdin.Write([]byte("foo\n"))
	c.S.Expect("foo")
	c.Shutdown(os.Interrupt)
	c.S.ExpectEOF()
}

func TestWithoutRuntime(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{PropagateChildOutput: true})
	defer sh.Cleanup()

	c := sh.Fn(withoutRuntime)
	c.ExitErrorIsOk = true
	c.Start()
	c.S.Expect("ready")
	// Processes without a Vanadium runtime should exit on SIGHUP.
	c.Shutdown(syscall.SIGHUP)
	c.S.ExpectEOF()
}
