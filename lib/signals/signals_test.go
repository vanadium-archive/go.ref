// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package signals

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/services/appcycle"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/mgmt"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/xrpc"
	"v.io/x/ref/services/device"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"

	_ "v.io/x/ref/runtime/factories/generic"
)

//go:generate v23 test generate

func stopLoop(stop func(), stdin io.Reader, ch chan<- struct{}) {
	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "close":
			close(ch)
			return
		case "stop":
			stop()
		}
	}
}

func program(stdin io.Reader, stdout io.Writer, signals ...os.Signal) {
	ctx, shutdown := test.V23Init()
	closeStopLoop := make(chan struct{})
	// obtain ac here since stopLoop may execute after shutdown is called below
	ac := v23.GetAppCycle(ctx)
	go stopLoop(func() { ac.Stop(ctx) }, stdin, closeStopLoop)
	wait := ShutdownOnSignals(ctx, signals...)
	fmt.Fprintf(stdout, "ready\n")
	fmt.Fprintf(stdout, "received signal %s\n", <-wait)
	shutdown()
	<-closeStopLoop
}

var handleDefaults = modules.Register(func(env *modules.Env, args ...string) error {
	program(env.Stdin, env.Stdout)
	return nil
}, "handleDefaults")

var handleCustom = modules.Register(func(env *modules.Env, args ...string) error {
	program(env.Stdin, env.Stdout, syscall.SIGABRT)
	return nil
}, "handleCustom")

var handleCustomWithStop = modules.Register(func(env *modules.Env, args ...string) error {
	program(env.Stdin, env.Stdout, STOP, syscall.SIGABRT, syscall.SIGHUP)
	return nil
}, "handleCustomWithStop")

var handleDefaultsIgnoreChan = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	closeStopLoop := make(chan struct{})
	// obtain ac here since stopLoop may execute after shutdown is called below
	ac := v23.GetAppCycle(ctx)
	go stopLoop(func() { ac.Stop(ctx) }, env.Stdin, closeStopLoop)
	ShutdownOnSignals(ctx)
	fmt.Fprintf(env.Stdout, "ready\n")
	<-closeStopLoop
	return nil
}, "handleDefaultsIgnoreChan")

func isSignalInSet(sig os.Signal, set []os.Signal) bool {
	for _, s := range set {
		if sig == s {
			return true
		}
	}
	return false
}

func checkSignalIsDefault(t *testing.T, sig os.Signal) {
	if !isSignalInSet(sig, defaultSignals()) {
		t.Errorf("signal %s not in default signal set, as expected", sig)
	}
}

func checkSignalIsNotDefault(t *testing.T, sig os.Signal) {
	if isSignalInSet(sig, defaultSignals()) {
		t.Errorf("signal %s unexpectedly in default signal set", sig)
	}
}

func newShell(t *testing.T, ctx *context.T, prog modules.Program) (*modules.Shell, modules.Handle) {
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	handle, err := sh.Start(nil, prog)
	if err != nil {
		sh.Cleanup(os.Stderr, os.Stderr)
		t.Fatalf("unexpected error: %s", err)
		return nil, nil
	}
	return sh, handle
}

// TestCleanShutdownSignal verifies that sending a signal to a child that
// handles it by default causes the child to shut down cleanly.
func TestCleanShutdownSignal(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaults)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(h.Pid(), syscall.SIGINT)
	h.Expectf("received signal %s", syscall.SIGINT)
	fmt.Fprintf(h.Stdin(), "close\n")
	h.ExpectEOF()
}

// TestCleanShutdownStop verifies that sending a stop comamnd to a child that
// handles stop commands by default causes the child to shut down cleanly.
func TestCleanShutdownStop(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaults)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	h.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(h.Stdin(), "close\n")
	h.ExpectEOF()

}

// TestCleanShutdownStopCustom verifies that sending a stop comamnd to a child
// that handles stop command as part of a custom set of signals handled, causes
// the child to shut down cleanly.
func TestCleanShutdownStopCustom(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleCustomWithStop)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	h.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(h.Stdin(), "close\n")
	h.ExpectEOF()
}

func testExitStatus(t *testing.T, h modules.Handle, code int) {
	h.ExpectEOF()
	_, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	if got, want := h.Shutdown(os.Stdout, os.Stderr), fmt.Errorf("exit status %d", code); got.Error() != want.Error() {
		t.Errorf("%s:%d: got %q, want %q", file, line, got, want)
	}
}

// TestStopNoHandler verifies that sending a stop command to a child that does
// not handle stop commands causes the child to exit immediately.
func TestStopNoHandler(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleCustom)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	testExitStatus(t, h, v23.UnhandledStopExitCode)
}

// TestDoubleSignal verifies that sending a succession of two signals to a child
// that handles these signals by default causes the child to exit immediately
// upon receiving the second signal.
func TestDoubleSignal(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaults)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(h.Pid(), syscall.SIGTERM)
	h.Expectf("received signal %s", syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(h.Pid(), syscall.SIGINT)
	testExitStatus(t, h, DoubleStopExitCode)
}

// TestSignalAndStop verifies that sending a signal followed by a stop command
// to a child that handles these by default causes the child to exit immediately
// upon receiving the stop command.
func TestSignalAndStop(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaults)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(h.Pid(), syscall.SIGTERM)
	h.Expectf("received signal %s", syscall.SIGTERM)
	fmt.Fprintf(h.Stdin(), "stop\n")
	testExitStatus(t, h, DoubleStopExitCode)
}

// TestDoubleStop verifies that sending a succession of stop commands to a child
// that handles stop commands by default causes the child to exit immediately
// upon receiving the second stop command.
func TestDoubleStop(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaults)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	h.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(h.Stdin(), "stop\n")
	testExitStatus(t, h, DoubleStopExitCode)
}

// TestSendUnhandledSignal verifies that sending a signal that the child does
// not handle causes the child to exit as per the signal being sent.
func TestSendUnhandledSignal(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaults)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(h.Pid(), syscall.SIGABRT)
	testExitStatus(t, h, 2)
}

// TestDoubleSignalIgnoreChan verifies that, even if we ignore the channel that
// ShutdownOnSignals returns, sending two signals should still cause the
// process to exit (ensures that there is no dependency in ShutdownOnSignals
// on having a goroutine read from the returned channel).
func TestDoubleSignalIgnoreChan(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleDefaultsIgnoreChan)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	// Even if we ignore the channel that ShutdownOnSignals returns,
	// sending two signals should still cause the process to exit.
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(h.Pid(), syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(h.Pid(), syscall.SIGINT)
	testExitStatus(t, h, DoubleStopExitCode)
}

// TestHandlerCustomSignal verifies that sending a non-default signal to a
// server that listens for that signal causes the server to shut down cleanly.
func TestHandlerCustomSignal(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, h := newShell(t, ctx, handleCustom)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(h.Pid(), syscall.SIGABRT)
	h.Expectf("received signal %s", syscall.SIGABRT)
	fmt.Fprintf(h.Stdin(), "stop\n")
	h.ExpectEOF()
}

// TestHandlerCustomSignalWithStop verifies that sending a custom stop signal
// to a server that listens for that signal causes the server to shut down
// cleanly, even when a STOP signal is also among the handled signals.
func TestHandlerCustomSignalWithStop(t *testing.T) {
	rootCtx, shutdown := test.V23Init()
	defer shutdown()

	for _, signal := range []syscall.Signal{syscall.SIGABRT, syscall.SIGHUP} {
		ctx, _ := vtrace.WithNewTrace(rootCtx)
		sh, h := newShell(t, ctx, handleCustomWithStop)
		h.Expect("ready")
		checkSignalIsNotDefault(t, signal)
		syscall.Kill(h.Pid(), signal)
		h.Expectf("received signal %s", signal)
		fmt.Fprintf(h.Stdin(), "close\n")
		h.ExpectEOF()
		sh.Cleanup(os.Stderr, os.Stderr)
	}
}

// TestParseSignalsList verifies that ShutdownOnSignals correctly interprets
// the input list of signals.
func TestParseSignalsList(t *testing.T) {
	list := []os.Signal{STOP, syscall.SIGTERM}
	ShutdownOnSignals(nil, list...)
	if !isSignalInSet(syscall.SIGTERM, list) {
		t.Errorf("signal %s not in signal set, as expected: %v", syscall.SIGTERM, list)
	}
	if !isSignalInSet(STOP, list) {
		t.Errorf("signal %s not in signal set, as expected: %v", STOP, list)
	}
}

type configServer struct {
	ch chan<- string
}

func (c *configServer) Set(_ *context.T, _ rpc.ServerCall, key, value string) error {
	if key != mgmt.AppCycleManagerConfigKey {
		return fmt.Errorf("Unexpected key: %v", key)
	}
	c.ch <- value
	return nil

}

// TestCleanRemoteShutdown verifies that remote shutdown works correctly.
func TestCleanRemoteShutdown(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	ch := make(chan string)
	server, err := xrpc.NewServer(ctx, "", device.ConfigServer(&configServer{ch}), securityflag.NewAuthorizerOrDie())
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	configServiceName := server.Status().Endpoints[0].Name()

	sh.SetConfigKey(mgmt.ParentNameConfigKey, configServiceName)
	sh.SetConfigKey(mgmt.ProtocolConfigKey, "tcp")
	sh.SetConfigKey(mgmt.AddressConfigKey, "127.0.0.1:0")
	h, err := sh.Start(nil, handleDefaults)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	appCycleName := <-ch
	h.Expect("ready")
	appCycle := appcycle.AppCycleClient(appCycleName)
	stream, err := appCycle.Stop(ctx)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	rStream := stream.RecvStream()
	if rStream.Advance() || rStream.Err() != nil {
		t.Errorf("Expected EOF, got (%v, %v) instead: ", rStream.Value(), rStream.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	h.Expectf("received signal %s", v23.RemoteStop)
	fmt.Fprintf(h.Stdin(), "close\n")
	h.ExpectEOF()
}
