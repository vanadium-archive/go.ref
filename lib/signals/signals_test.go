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
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/services/appcycle"
	"v.io/x/lib/gosh"
	"v.io/x/ref/lib/mgmt"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/v23test"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/device"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
)

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

func program(signals ...os.Signal) {
	ctx, shutdown := test.V23Init()
	closeStopLoop := make(chan struct{})
	// obtain ac here since stopLoop may execute after shutdown is called below
	ac := v23.GetAppCycle(ctx)
	go stopLoop(func() { ac.Stop(ctx) }, os.Stdin, closeStopLoop)
	wait := ShutdownOnSignals(ctx, signals...)
	fmt.Printf("ready\n")
	fmt.Printf("received signal %s\n", <-wait)
	shutdown()
	<-closeStopLoop
}

var handleDefaults = gosh.Register("handleDefaults", func() {
	program()
})

var handleCustom = gosh.Register("handleCustom", func() {
	program(syscall.SIGABRT)
})

var handleCustomWithStop = gosh.Register("handleCustomWithStop", func() {
	program(STOP, syscall.SIGABRT, syscall.SIGHUP)
})

var handleDefaultsIgnoreChan = gosh.Register("handleDefaultsIgnoreChan", func() {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	closeStopLoop := make(chan struct{})
	// obtain ac here since stopLoop may execute after shutdown is called below
	ac := v23.GetAppCycle(ctx)
	go stopLoop(func() { ac.Stop(ctx) }, os.Stdin, closeStopLoop)
	ShutdownOnSignals(ctx)
	fmt.Printf("ready\n")
	<-closeStopLoop
})

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

func startFn(t *testing.T, sh *v23test.Shell, fn *gosh.Fn, exitErrorIsOk bool) (*v23test.Cmd, *expect.Session, io.WriteCloser) {
	cmd := sh.Fn(fn)
	pr, pw := io.Pipe()
	cmd.Stdin = pr
	session := expect.NewSession(t, cmd.StdoutPipe(), 5*time.Second)
	cmd.ExitErrorIsOk = true
	cmd.Start()
	return cmd, session, pw
}

func checkEOF(cmd *v23test.Cmd, session *expect.Session, stdinPipe io.WriteCloser) {
	stdinPipe.Close()
	cmd.Wait()
	session.ExpectEOF()
}

// TestCleanShutdownSignal verifies that sending a signal to a child that
// handles it by default causes the child to shut down cleanly.
func TestCleanShutdownSignal(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaults, false)
	session.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(cmd.Process().Pid, syscall.SIGINT)
	session.Expectf("received signal %s", syscall.SIGINT)
	fmt.Fprintf(stdinPipe, "close\n")
	checkEOF(cmd, session, stdinPipe)
}

// TestCleanShutdownStop verifies that sending a stop command to a child that
// handles stop commands by default causes the child to shut down cleanly.
func TestCleanShutdownStop(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaults, false)
	session.Expect("ready")
	fmt.Fprintf(stdinPipe, "stop\n")
	session.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(stdinPipe, "close\n")
	checkEOF(cmd, session, stdinPipe)
}

// TestCleanShutdownStopCustom verifies that sending a stop command to a child
// that handles stop command as part of a custom set of signals handled, causes
// the child to shut down cleanly.
func TestCleanShutdownStopCustom(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleCustomWithStop, false)
	session.Expect("ready")
	fmt.Fprintf(stdinPipe, "stop\n")
	session.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(stdinPipe, "close\n")
	checkEOF(cmd, session, stdinPipe)
}

func checkExitStatus(t *testing.T, cmd *v23test.Cmd, code int) {
	if got, want := cmd.Err, fmt.Errorf("exit status %d", code); got.Error() != want.Error() {
		_, file, line, _ := runtime.Caller(1)
		file = filepath.Base(file)
		t.Errorf("%s:%d: got %q, want %q", file, line, got, want)
	}
}

// TestStopNoHandler verifies that sending a stop command to a child that does
// not handle stop commands causes the child to exit immediately.
func TestStopNoHandler(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleCustom, true)
	session.Expect("ready")
	fmt.Fprintf(stdinPipe, "stop\n")
	checkEOF(cmd, session, stdinPipe)
	checkExitStatus(t, cmd, v23.UnhandledStopExitCode)
}

// TestDoubleSignal verifies that sending a succession of two signals to a child
// that handles these signals by default causes the child to exit immediately
// upon receiving the second signal.
func TestDoubleSignal(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaults, true)
	session.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(cmd.Process().Pid, syscall.SIGTERM)
	session.Expectf("received signal %s", syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(cmd.Process().Pid, syscall.SIGINT)
	checkEOF(cmd, session, stdinPipe)
	checkExitStatus(t, cmd, DoubleStopExitCode)
}

// TestSignalAndStop verifies that sending a signal followed by a stop command
// to a child that handles these by default causes the child to exit immediately
// upon receiving the stop command.
func TestSignalAndStop(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaults, true)
	session.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(cmd.Process().Pid, syscall.SIGTERM)
	session.Expectf("received signal %s", syscall.SIGTERM)
	fmt.Fprintf(stdinPipe, "stop\n")
	checkEOF(cmd, session, stdinPipe)
	checkExitStatus(t, cmd, DoubleStopExitCode)
}

// TestDoubleStop verifies that sending a succession of stop commands to a child
// that handles stop commands by default causes the child to exit immediately
// upon receiving the second stop command.
func TestDoubleStop(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaults, true)
	session.Expect("ready")
	fmt.Fprintf(stdinPipe, "stop\n")
	session.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(stdinPipe, "stop\n")
	checkEOF(cmd, session, stdinPipe)
	checkExitStatus(t, cmd, DoubleStopExitCode)
}

// TestSendUnhandledSignal verifies that sending a signal that the child does
// not handle causes the child to exit as per the signal being sent.
func TestSendUnhandledSignal(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaults, true)
	session.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(cmd.Process().Pid, syscall.SIGABRT)
	checkEOF(cmd, session, stdinPipe)
	checkExitStatus(t, cmd, 2)
}

// TestDoubleSignalIgnoreChan verifies that, even if we ignore the channel that
// ShutdownOnSignals returns, sending two signals should still cause the
// process to exit (ensures that there is no dependency in ShutdownOnSignals
// on having a goroutine read from the returned channel).
func TestDoubleSignalIgnoreChan(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleDefaultsIgnoreChan, true)
	session.Expect("ready")
	// Even if we ignore the channel that ShutdownOnSignals returns,
	// sending two signals should still cause the process to exit.
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(cmd.Process().Pid, syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(cmd.Process().Pid, syscall.SIGINT)
	checkEOF(cmd, session, stdinPipe)
	checkExitStatus(t, cmd, DoubleStopExitCode)
}

// TestHandlerCustomSignal verifies that sending a non-default signal to a
// server that listens for that signal causes the server to shut down cleanly.
func TestHandlerCustomSignal(t *testing.T) {
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	cmd, session, stdinPipe := startFn(t, sh, handleCustom, true)
	session.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(cmd.Process().Pid, syscall.SIGABRT)
	session.Expectf("received signal %s", syscall.SIGABRT)
	fmt.Fprintf(stdinPipe, "stop\n")
	checkEOF(cmd, session, stdinPipe)
}

// TestHandlerCustomSignalWithStop verifies that sending a custom stop signal
// to a server that listens for that signal causes the server to shut down
// cleanly, even when a STOP signal is also among the handled signals.
func TestHandlerCustomSignalWithStop(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGABRT, syscall.SIGHUP} {
		func() {
			sh := v23test.NewShell(t, v23test.Opts{})
			defer sh.Cleanup()

			cmd, session, stdinPipe := startFn(t, sh, handleCustomWithStop, true)
			session.Expect("ready")
			checkSignalIsNotDefault(t, signal)
			syscall.Kill(cmd.Process().Pid, signal)
			session.Expectf("received signal %s", signal)
			fmt.Fprintf(stdinPipe, "close\n")
			checkEOF(cmd, session, stdinPipe)
		}()
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

func modulesProgram(stdin io.Reader, stdout io.Writer, signals ...os.Signal) {
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

var modulesHandleDefaults = modules.Register(func(env *modules.Env, args ...string) error {
	modulesProgram(env.Stdin, env.Stdout)
	return nil
}, "modulesHandleDefaults")

// TestCleanRemoteShutdown verifies that remote shutdown works correctly.
// TODO(caprita): Rewrite this test to not use the modules package.
func TestCleanRemoteShutdown(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	ch := make(chan string)
	_, server, err := v23.WithNewServer(ctx, "", device.ConfigServer(&configServer{ch}), securityflag.NewAuthorizerOrDie())
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	configServiceName := server.Status().Endpoints[0].Name()

	sh.SetConfigKey(mgmt.ParentNameConfigKey, configServiceName)
	sh.SetConfigKey(mgmt.ProtocolConfigKey, "tcp")
	sh.SetConfigKey(mgmt.AddressConfigKey, "127.0.0.1:0")
	h, err := sh.Start(nil, modulesHandleDefaults)
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

func TestMain(m *testing.M) {
	modules.DispatchAndExitIfChild()
	os.Exit(v23test.Run(m.Run))
}
