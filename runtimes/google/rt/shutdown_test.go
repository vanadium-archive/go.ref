package rt_test

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron2"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/lib/testutil"
)

var cstderr io.Writer

func init() {
	testutil.Init()
	if testing.Verbose() {
		cstderr = os.Stderr
	}
}

func newShell(t *testing.T) *modules.Shell {
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	return sh
}

// TestSimpleServerSignal verifies that sending a signal to the simple server
// causes it to exit cleanly.
func TestSimpleServerSignal(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("simpleServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	syscall.Kill(h.Pid(), syscall.SIGINT)
	s.Expect("Received signal interrupt")
	s.Expect("Interruptible cleanup")
	s.Expect("Deferred cleanup")
	fmt.Fprintln(h.Stdin(), "close")
	s.ExpectEOF()
}

// TestSimpleServerLocalStop verifies that sending a local stop command to the
// simple server causes it to exit cleanly.
func TestSimpleServerLocalStop(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("simpleServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	fmt.Fprintln(h.Stdin(), "stop")
	s.Expect(fmt.Sprintf("Received signal %s", veyron2.LocalStop))
	s.Expect("Interruptible cleanup")
	s.Expect("Deferred cleanup")
	fmt.Fprintln(h.Stdin(), "close")
	s.ExpectEOF()
}

// TestSimpleServerDoubleSignal verifies that sending a succession of two
// signals to the simple server causes it to initiate the cleanup sequence on
// the first signal and then exit immediately on the second signal.
func TestSimpleServerDoubleSignal(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("simpleServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	syscall.Kill(h.Pid(), syscall.SIGINT)
	s.Expect("Received signal interrupt")
	syscall.Kill(h.Pid(), syscall.SIGINT)
	err := h.Shutdown(os.Stdout, cstderr)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got, want := err.Error(), fmt.Sprintf("exit status %d", signals.DoubleStopExitCode); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSimpleServerLocalForceStop verifies that sending a local ForceStop
// command to the simple server causes it to exit immediately.
func TestSimpleServerLocalForceStop(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("simpleServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	fmt.Fprintln(h.Stdin(), "forcestop")
	s.Expect("straight exit")
	err := h.Shutdown(os.Stdout, cstderr)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got, want := err.Error(), fmt.Sprintf("exit status %d", veyron2.ForceStopExitCode); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSimpleServerKill demonstrates that a SIGKILL still forces the server
// to exit regardless of our signal handling.
func TestSimpleServerKill(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("simpleServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	syscall.Kill(h.Pid(), syscall.SIGKILL)
	err := h.Shutdown(os.Stdout, cstderr)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got, want := err.Error(), "signal: killed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComplexServerSignal verifies that sending a signal to the complex server
// initiates the cleanup sequence in that server (we observe the printouts
// corresponding to all the simulated sequential/parallel and
// blocking/interruptible shutdown steps), and then exits cleanly.
func TestComplexServerSignal(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("complexServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	syscall.Kill(h.Pid(), syscall.SIGINT)
	s.Expect("Received signal interrupt")
	s.ExpectSetRE("Sequential blocking cleanup",
		"Sequential interruptible cleanup",
		"Parallel blocking cleanup1",
		"Parallel blocking cleanup2",
		"Parallel interruptible cleanup1",
		"Parallel interruptible cleanup2")
	fmt.Fprintln(h.Stdin(), "close")
	s.ExpectEOF()
}

// TestComplexServerLocalStop verifies that sending a local stop command to the
// complex server initiates the cleanup sequence in that server (we observe the
// printouts corresponding to all the simulated sequential/parallel and
// blocking/interruptible shutdown steps), and then exits cleanly.
func TestComplexServerLocalStop(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("complexServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")

	fmt.Fprintln(h.Stdin(), "stop")
	s.Expect(fmt.Sprintf("Stop %s", veyron2.LocalStop))
	s.ExpectSetRE(
		"Sequential blocking cleanup",
		"Sequential interruptible cleanup",
		"Parallel blocking cleanup1",
		"Parallel blocking cleanup2",
		"Parallel interruptible cleanup1",
		"Parallel interruptible cleanup2",
	)
	fmt.Fprintln(h.Stdin(), "close")
	s.ExpectEOF()
}

// TestComplexServerDoubleSignal verifies that sending a succession of two
// signals to the complex server has the expected effect: the first signal
// initiates the cleanup steps and the second signal kills the process, but only
// after the blocking shutdown steps were allowed to complete (as observed by
// the corresponding printouts from the server).  Note that we have no
// expectations on whether or not the interruptible shutdown steps execute.
func TestComplexServerDoubleSignal(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("complexServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	syscall.Kill(h.Pid(), syscall.SIGINT)
	s.Expect("Received signal interrupt")
	syscall.Kill(h.Pid(), syscall.SIGINT)
	s.ExpectSetEventuallyRE(
		"Sequential blocking cleanup",
		"Parallel blocking cleanup1",
		"Parallel blocking cleanup2")
	err := h.Shutdown(os.Stdout, cstderr)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got, want := err.Error(), fmt.Sprintf("exit status %d", signals.DoubleStopExitCode); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComplexServerLocalForceStop verifies that sending a local ForceStop
// command to the complex server forces it to exit immediately.
func TestComplexServerLocalForceStop(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("complexServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	fmt.Fprintln(h.Stdin(), "forcestop")
	s.Expect("straight exit")
	err := h.Shutdown(os.Stdout, cstderr)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got, want := err.Error(), fmt.Sprintf("exit status %d", veyron2.ForceStopExitCode); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestComplexServerKill demonstrates that a SIGKILL still forces the server to
// exit regardless of our signal handling.
func TestComplexServerKill(t *testing.T) {
	sh := newShell(t)
	defer sh.Cleanup(os.Stdout, cstderr)
	h, _ := sh.Start("complexServerProgram", nil)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("Ready")
	syscall.Kill(h.Pid(), syscall.SIGKILL)
	err := h.Shutdown(os.Stdout, cstderr)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got, want := err.Error(), "signal: killed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
