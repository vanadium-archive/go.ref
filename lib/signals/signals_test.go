package signals

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"veyron2"
	"veyron2/rt"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
)

// TestHelperProcess is boilerplate for the blackbox setup.
func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func init() {
	blackbox.CommandTable["handleDefaults"] = handleDefaults
	blackbox.CommandTable["handleCustom"] = handleCustom
	blackbox.CommandTable["handleDefaultsIgnoreChan"] = handleDefaultsIgnoreChan
}

func stopLoop(ch chan<- struct{}) {
	for {
		switch blackbox.ReadLineFromStdin() {
		case "close":
			close(ch)
			return
		case "stop":
			rt.R().Stop()
		}
	}
}

func program(signals ...os.Signal) {
	defer rt.Init().Shutdown()
	closeStopLoop := make(chan struct{})
	go stopLoop(closeStopLoop)
	wait := ShutdownOnSignals(signals...)
	fmt.Println("ready")
	fmt.Println("received signal", <-wait)
	<-closeStopLoop
}

func handleDefaults([]string) {
	program()
}

func handleCustom([]string) {
	program(syscall.SIGABRT)
}

func handleDefaultsIgnoreChan([]string) {
	defer rt.Init().Shutdown()
	closeStopLoop := make(chan struct{})
	go stopLoop(closeStopLoop)
	ShutdownOnSignals()
	fmt.Println("ready")
	<-closeStopLoop
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

// TestCleanShutdownSignal verifies that sending a signal to a child that
// handles it by default causes the child to shut down cleanly.
func TestCleanShutdownSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.Expect(fmt.Sprintf("received signal %s", syscall.SIGINT))
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}

// TestCleanShutdownStop verifies that sending a stop comamnd to a child that
// handles stop commands by default causes the child to shut down cleanly.
func TestCleanShutdownStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	c.WriteLine("stop")
	c.Expect(fmt.Sprintf("received signal %s", veyron2.LocalStop))
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}

// TestStopNoHandler verifies that sending a stop command to a child that does
// not handle stop commands causes the child to exit immediately.
func TestStopNoHandler(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleCustom")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	c.WriteLine("stop")
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", veyron2.UnhandledStopExitCode))
}

// TestDoubleSignal verifies that sending a succession of two signals to a child
// that handles these signals by default causes the child to exit immediately
// upon receiving the second signal.
func TestDoubleSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGTERM)
	c.Expect(fmt.Sprintf("received signal %s", syscall.SIGTERM))
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", DoubleStopExitCode))
}

// TestSignalAndStop verifies that sending a signal followed by a stop command
// to a child that handles these by default causes the child to exit immediately
// upon receiving the stop command.
func TestSignalAndStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGTERM)
	c.Expect(fmt.Sprintf("received signal %s", syscall.SIGTERM))
	c.WriteLine("stop")
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", DoubleStopExitCode))
}

// TestDoubleStop verifies that sending a succession of stop commands to a child
// that handles stop commands by default causes the child to exit immediately
// upon receiving the second stop command.
func TestDoubleStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	c.WriteLine("stop")
	c.Expect(fmt.Sprintf("received signal %s", veyron2.LocalStop))
	c.WriteLine("stop")
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", DoubleStopExitCode))
}

// TestSendUnhandledSignal verifies that sending a signal that the child does
// not handle causes the child to exit as per the signal being sent.
func TestSendUnhandledSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGABRT)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status 2"))
}

// TestDoubleSignalIgnoreChan verifies that, even if we ignore the channel that
// ShutdownOnSignals returns, sending two signals should still cause the
// process to exit (ensures that there is no dependency in ShutdownOnSignals
// on having a goroutine read from the returned channel).
func TestDoubleSignalIgnoreChan(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleDefaultsIgnoreChan")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	// Even if we ignore the channel that ShutdownOnSignals returns,
	// sending two signals should still cause the process to exit.
	checkSignalIsDefault(t, syscall.SIGTERM)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGTERM)
	checkSignalIsDefault(t, syscall.SIGINT)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGINT)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", DoubleStopExitCode))
}

// TestHandlerCustomSignal verifies that sending a non-default signal to a
// server that listens for that signal causes the server to shut down cleanly.
func TestHandlerCustomSignal(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleCustom")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	checkSignalIsNotDefault(t, syscall.SIGABRT)
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGABRT)
	c.Expect(fmt.Sprintf("received signal %s", syscall.SIGABRT))
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}
