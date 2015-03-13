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
	"v.io/v23/ipc"
	"v.io/v23/mgmt"
	"v.io/v23/naming"
	"v.io/v23/services/mgmt/appcycle"
	"v.io/v23/vtrace"

	_ "v.io/x/ref/profiles"
	vflag "v.io/x/ref/security/flag"
	"v.io/x/ref/services/mgmt/device"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
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
	ctx, shutdown := test.InitForTest()
	closeStopLoop := make(chan struct{})
	go stopLoop(v23.GetAppCycle(ctx).Stop, stdin, closeStopLoop)
	wait := ShutdownOnSignals(ctx, signals...)
	fmt.Fprintf(stdout, "ready\n")
	fmt.Fprintf(stdout, "received signal %s\n", <-wait)
	shutdown()
	<-closeStopLoop
}

func handleDefaults(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	program(stdin, stdout)
	return nil
}

func handleCustom(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	program(stdin, stdout, syscall.SIGABRT)
	return nil
}

func handleCustomWithStop(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	program(stdin, stdout, STOP, syscall.SIGABRT, syscall.SIGHUP)
	return nil
}

func handleDefaultsIgnoreChan(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	closeStopLoop := make(chan struct{})
	go stopLoop(v23.GetAppCycle(ctx).Stop, stdin, closeStopLoop)
	ShutdownOnSignals(ctx)
	fmt.Fprintf(stdout, "ready\n")
	<-closeStopLoop
	return nil
}

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

func newShell(t *testing.T, ctx *context.T, command string) (*modules.Shell, modules.Handle, *expect.Session) {
	sh, err := modules.NewShell(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	handle, err := sh.Start(command, nil)
	if err != nil {
		sh.Cleanup(os.Stderr, os.Stderr)
		t.Fatalf("unexpected error: %s", err)
		return nil, nil, nil
	}
	session := expect.NewSession(t, handle.Stdout(), time.Minute)
	return sh, handle, session
}

// TestCleanShutdownSignal verifies that sending a signal to a child that
// handles it by default causes the child to shut down cleanly.
func TestCleanShutdownSignal(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaults")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(h.Pid(), syscall.SIGINT)
	s.Expectf("received signal %s", syscall.SIGINT)
	fmt.Fprintf(h.Stdin(), "close\n")
	s.ExpectEOF()
}

// TestCleanShutdownStop verifies that sending a stop comamnd to a child that
// handles stop commands by default causes the child to shut down cleanly.
func TestCleanShutdownStop(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaults")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	s.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(h.Stdin(), "close\n")
	s.ExpectEOF()

}

// TestCleanShutdownStopCustom verifies that sending a stop comamnd to a child
// that handles stop command as part of a custom set of signals handled, causes
// the child to shut down cleanly.
func TestCleanShutdownStopCustom(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleCustomWithStop")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	s.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(h.Stdin(), "close\n")
	s.ExpectEOF()
}

func testExitStatus(t *testing.T, h modules.Handle, s *expect.Session, code int) {
	s.ExpectEOF()
	_, file, line, _ := runtime.Caller(1)
	file = filepath.Base(file)
	if got, want := h.Shutdown(os.Stdout, os.Stderr), fmt.Errorf("exit status %d", code); got.Error() != want.Error() {
		t.Errorf("%s:%d: got %q, want %q", file, line, got, want)
	}
}

// TestStopNoHandler verifies that sending a stop command to a child that does
// not handle stop commands causes the child to exit immediately.
func TestStopNoHandler(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleCustom")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	testExitStatus(t, h, s, v23.UnhandledStopExitCode)
}

// TestDoubleSignal verifies that sending a succession of two signals to a child
// that handles these signals by default causes the child to exit immediately
// upon receiving the second signal.
func TestDoubleSignal(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaults")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(h.Pid(), syscall.SIGTERM)
	s.Expectf("received signal %s", syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(h.Pid(), syscall.SIGINT)
	testExitStatus(t, h, s, DoubleStopExitCode)
}

// TestSignalAndStop verifies that sending a signal followed by a stop command
// to a child that handles these by default causes the child to exit immediately
// upon receiving the stop command.
func TestSignalAndStop(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaults")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(h.Pid(), syscall.SIGTERM)
	s.Expectf("received signal %s", syscall.SIGTERM)
	fmt.Fprintf(h.Stdin(), "stop\n")
	testExitStatus(t, h, s, DoubleStopExitCode)
}

// TestDoubleStop verifies that sending a succession of stop commands to a child
// that handles stop commands by default causes the child to exit immediately
// upon receiving the second stop command.
func TestDoubleStop(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaults")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	fmt.Fprintf(h.Stdin(), "stop\n")
	s.Expectf("received signal %s", v23.LocalStop)
	fmt.Fprintf(h.Stdin(), "stop\n")
	testExitStatus(t, h, s, DoubleStopExitCode)
}

// TestSendUnhandledSignal verifies that sending a signal that the child does
// not handle causes the child to exit as per the signal being sent.
func TestSendUnhandledSignal(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaults")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(h.Pid(), syscall.SIGABRT)
	testExitStatus(t, h, s, 2)
}

// TestDoubleSignalIgnoreChan verifies that, even if we ignore the channel that
// ShutdownOnSignals returns, sending two signals should still cause the
// process to exit (ensures that there is no dependency in ShutdownOnSignals
// on having a goroutine read from the returned channel).
func TestDoubleSignalIgnoreChan(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleDefaultsIgnoreChan")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	// Even if we ignore the channel that ShutdownOnSignals returns,
	// sending two signals should still cause the process to exit.
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(h.Pid(), syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(h.Pid(), syscall.SIGINT)
	testExitStatus(t, h, s, DoubleStopExitCode)
}

// TestHandlerCustomSignal verifies that sending a non-default signal to a
// server that listens for that signal causes the server to shut down cleanly.
func TestHandlerCustomSignal(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, h, s := newShell(t, ctx, "handleCustom")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	s.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(h.Pid(), syscall.SIGABRT)
	s.Expectf("received signal %s", syscall.SIGABRT)
	fmt.Fprintf(h.Stdin(), "stop\n")
	s.ExpectEOF()
}

// TestHandlerCustomSignalWithStop verifies that sending a custom stop signal
// to a server that listens for that signal causes the server to shut down
// cleanly, even when a STOP signal is also among the handled signals.
func TestHandlerCustomSignalWithStop(t *testing.T) {
	rootCtx, shutdown := test.InitForTest()
	defer shutdown()

	for _, signal := range []syscall.Signal{syscall.SIGABRT, syscall.SIGHUP} {
		ctx, _ := vtrace.SetNewTrace(rootCtx)
		sh, h, s := newShell(t, ctx, "handleCustomWithStop")
		s.Expect("ready")
		checkSignalIsNotDefault(t, signal)
		syscall.Kill(h.Pid(), signal)
		s.Expectf("received signal %s", signal)
		fmt.Fprintf(h.Stdin(), "close\n")
		s.ExpectEOF()
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

func (c *configServer) Set(_ ipc.ServerCall, key, value string) error {
	if key != mgmt.AppCycleManagerConfigKey {
		return fmt.Errorf("Unexpected key: %v", key)
	}
	c.ch <- value
	return nil

}

func createConfigServer(t *testing.T, ctx *context.T) (ipc.Server, string, <-chan string) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	ch := make(chan string)
	var ep []naming.Endpoint
	if ep, err = server.Listen(v23.GetListenSpec(ctx)); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	if err := server.Serve("", device.ConfigServer(&configServer{ch}), vflag.NewAuthorizerOrDie()); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	return server, ep[0].Name(), ch

}

// TestCleanRemoteShutdown verifies that remote shutdown works correctly.
func TestCleanRemoteShutdown(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	configServer, configServiceName, ch := createConfigServer(t, ctx)
	defer configServer.Stop()
	sh.SetConfigKey(mgmt.ParentNameConfigKey, configServiceName)
	sh.SetConfigKey(mgmt.ProtocolConfigKey, "tcp")
	sh.SetConfigKey(mgmt.AddressConfigKey, "127.0.0.1:0")
	h, err := sh.Start("handleDefaults", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	appCycleName := <-ch
	s.Expect("ready")
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
	s.Expectf("received signal %s", v23.RemoteStop)
	fmt.Fprintf(h.Stdin(), "close\n")
	s.ExpectEOF()
}
