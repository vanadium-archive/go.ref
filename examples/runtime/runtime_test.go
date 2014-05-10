package runtime

import (
	"fmt"
	"syscall"
	"testing"
	"time"

	"veyron/lib/signals"
	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron2/mgmt"
)

// TestHelperProcess is boilerplate for the blackbox setup.
func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func init() {
	blackbox.CommandTable["simpleServerProgram"] = func([]string) {
		// This is part of the test setup -- we need a way to accept
		// commands from the parent process to simulate Stop and
		// RemoteStop commands that would normally be issued from
		// application code.
		defer remoteCmdLoop()()
		simpleServerProgram()
	}
}

// TestSimpleServerSignal verifies that sending a signal to the simple server
// causes it to exit cleanly.
func TestSimpleServerSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "simpleServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.Expect("Received signal interrupt")
	c.Expect("Interruptible cleanup")
	c.Expect("Deferred cleanup")
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}

// TestSimpleServerLocalStop verifies that sending a local stop command to the
// simple server causes it to exit cleanly.
func TestSimpleServerLocalStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "simpleServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	c.WriteLine("stop")
	c.Expect(fmt.Sprintf("Received signal %s", mgmt.LocalStop))
	c.Expect("Interruptible cleanup")
	c.Expect("Deferred cleanup")
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}

// TestSimpleServerDoubleSignal verifies that sending a succession of two
// signals to the simple server causes it to initiate the cleanup sequence on
// the first signal and then exit immediately on the second signal.
func TestSimpleServerDoubleSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "simpleServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.Expect("Received signal interrupt")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.WaitForEOF(time.Second)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", signals.DoubleStopExitCode))
}

// TestSimpleServerLocalForceStop verifies that sending a local ForceStop
// command to the simple server causes it to exit immediately.
func TestSimpleServerLocalForceStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "simpleServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	c.WriteLine("forcestop")
	c.Expect("straight exit")
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", mgmt.ForceStopExitCode))
}

// TestSimpleServerKill demonstrates that a SIGKILL still forces the server
// to exit regardless of our signal handling.
func TestSimpleServerKill(t *testing.T) {
	c := blackbox.HelperCommand(t, "simpleServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGKILL)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("signal: killed"))
}

func init() {
	blackbox.CommandTable["complexServerProgram"] = func([]string) {
		// This is part of the test setup -- we need a way to accept
		// commands from the parent process to simulate Stop and
		// RemoteStop commands that would normally be issued from
		// application code.
		defer remoteCmdLoop()()
		complexServerProgram()
	}
}

// TestComplexServerSignal verifies that sending a signal to the complex server
// initiates the cleanup sequence in that server (we observe the printouts
// corresponding to all the simulated sequential/parallel and
// blocking/interruptible shutdown steps), and then exits cleanly.
func TestComplexServerSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "complexServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.Expect("Received signal interrupt")
	c.ExpectSet([]string{
		"Sequential blocking cleanup",
		"Sequential interruptible cleanup",
		"Parallel blocking cleanup1",
		"Parallel blocking cleanup2",
		"Parallel interruptible cleanup1",
		"Parallel interruptible cleanup2",
	})
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}

// TestComplexServerLocalStop verifies that sending a local stop command to the
// complex server initiates the cleanup sequence in that server (we observe the
// printouts corresponding to all the simulated sequential/parallel and
// blocking/interruptible shutdown steps), and then exits cleanly.
func TestComplexServerLocalStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "complexServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	c.WriteLine("stop")
	c.Expect(fmt.Sprintf("Stop %s", mgmt.LocalStop))
	c.ExpectSet([]string{
		"Sequential blocking cleanup",
		"Sequential interruptible cleanup",
		"Parallel blocking cleanup1",
		"Parallel blocking cleanup2",
		"Parallel interruptible cleanup1",
		"Parallel interruptible cleanup2",
	})
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}

// TestComplexServerDoubleSignal verifies that sending a succession of two
// signals to the complex server has the expected effect: the first signal
// initiates the cleanup steps and the second signal kills the process, but only
// after the blocking shutdown steps were allowed to complete (as observed by
// the corresponding printouts from the server).  Note that we have no
// expectations on whether or not the interruptible shutdown steps execute.
func TestComplexServerDoubleSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "complexServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.Expect("Received signal interrupt")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.ExpectSetEventually([]string{
		"Sequential blocking cleanup",
		"Parallel blocking cleanup1",
		"Parallel blocking cleanup2",
	}, time.Second)
	c.WaitForEOF(time.Second)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", signals.DoubleStopExitCode))
}

// TestComplexServerLocalForceStop verifies that sending a local ForceStop
// command to the complex server forces it to exit immediately.
func TestComplexServerLocalForceStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "complexServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	c.WriteLine("forcestop")
	c.Expect("straight exit")
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", mgmt.ForceStopExitCode))
}

// TestComplexServerKill demonstrates that a SIGKILL still forces the server to
// exit regardless of our signal handling.
func TestComplexServerKill(t *testing.T) {
	c := blackbox.HelperCommand(t, "complexServerProgram")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("Ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGKILL)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("signal: killed"))
}

// TODO(caprita): Also demonstrate an example client application.
