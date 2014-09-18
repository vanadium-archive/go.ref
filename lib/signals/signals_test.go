package signals

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/mgmt"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/appcycle"

	_ "veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/lib/testutil/blackbox"
	"veyron.io/veyron/veyron/lib/testutil/security"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/node"
)

// TestHelperProcess is boilerplate for the blackbox setup.
func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func init() {
	blackbox.CommandTable["handleDefaults"] = handleDefaults
	blackbox.CommandTable["handleCustom"] = handleCustom
	blackbox.CommandTable["handleCustomWithStop"] = handleCustomWithStop
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
	r := rt.Init()
	closeStopLoop := make(chan struct{})
	go stopLoop(closeStopLoop)
	wait := ShutdownOnSignals(signals...)
	fmt.Println("ready")
	fmt.Println("received signal", <-wait)
	r.Cleanup()
	<-closeStopLoop
}

func handleDefaults([]string) {
	program()
}

func handleCustom([]string) {
	program(syscall.SIGABRT)
}

func handleCustomWithStop([]string) {
	program(STOP, syscall.SIGABRT, syscall.SIGHUP)
}

func handleDefaultsIgnoreChan([]string) {
	defer rt.Init().Cleanup()
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

// TestCleanShutdownStopCustom verifies that sending a stop comamnd to a child
// that handles stop command as part of a custom set of signals handled, causes
// the child to shut down cleanly.
func TestCleanShutdownStopCustom(t *testing.T) {
	c := blackbox.HelperCommand(t, "handleCustomWithStop")
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

// TestHandlerCustomSignalWithStop verifies that sending a custom stop signal
// to a server that listens for that signal causes the server to shut down
// cleanly, even when a STOP signal is also among the handled signals.
func TestHandlerCustomSignalWithStop(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGABRT, syscall.SIGHUP} {
		c := blackbox.HelperCommand(t, "handleCustomWithStop")
		c.Cmd.Start()
		c.Expect("ready")
		checkSignalIsNotDefault(t, signal)
		syscall.Kill(c.Cmd.Process.Pid, signal)
		c.Expect(fmt.Sprintf("received signal %s", signal))
		c.WriteLine("close")
		c.ExpectEOFAndWait()
		c.Cleanup()
	}
}

// TestParseSignalsList verifies that ShutdownOnSignals correctly interprets
// the input list of signals.
func TestParseSignalsList(t *testing.T) {
	list := []os.Signal{STOP, syscall.SIGTERM}
	ShutdownOnSignals(list...)
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

func (c *configServer) Set(_ ipc.ServerContext, key, value string) error {
	if key != mgmt.AppCycleManagerConfigKey {
		return fmt.Errorf("Unexpected key: %v", key)
	}
	c.ch <- value
	return nil

}

func createConfigServer(t *testing.T) (ipc.Server, string, <-chan string) {
	server, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	ch := make(chan string)
	var ep naming.Endpoint
	if ep, err = server.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	if err := server.Serve("", ipc.LeafDispatcher(node.NewServerConfig(&configServer{ch}), vflag.NewAuthorizerOrDie())); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	return server, naming.JoinAddressName(ep.String(), ""), ch

}

// TestCleanRemoteShutdown verifies that remote shutdown works correctly.
func TestCleanRemoteShutdown(t *testing.T) {
	r := rt.Init()
	defer r.Cleanup()
	c := blackbox.HelperCommand(t, "handleDefaults")
	defer c.Cleanup()
	// This sets up the child's identity to be derived from the parent's (so
	// that default authorization works for RPCs between the two).
	// TODO(caprita): Consider making this boilerplate part of blackbox.
	id := r.Identity()
	idFile := security.SaveIdentityToFile(security.NewBlessedIdentity(id, "test"))
	defer os.Remove(idFile)
	configServer, configServiceName, ch := createConfigServer(t)
	defer configServer.Stop()
	c.Cmd.Env = append(c.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile),
		fmt.Sprintf("%v=%v", mgmt.ParentNodeManagerConfigKey, configServiceName))
	c.Cmd.Start()
	appCycleName := <-ch
	c.Expect("ready")
	appCycle, err := appcycle.BindAppCycle(appCycleName)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	stream, err := appCycle.Stop(r.NewContext())
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
	c.Expect(fmt.Sprintf("received signal %s", veyron2.RemoteStop))
	c.WriteLine("close")
	c.ExpectEOFAndWait()
}
