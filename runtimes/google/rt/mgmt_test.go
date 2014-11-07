package rt_test

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/mgmt"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/services/mgmt/appcycle"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/lib/testutil/security"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/runtimes/google/rt"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/node"
)

var profileOpt = options.Profile{profiles.New()}

const (
	noWaitersCmd = "noWaiters"
	forceStopCmd = "forceStop"
	appCmd       = "app"
)

func init() {
	testutil.Init()
	modules.RegisterChild(noWaitersCmd, "", noWaiters)
	modules.RegisterChild(forceStopCmd, "", forceStop)
	modules.RegisterChild(appCmd, "", app)
}

// TestBasic verifies that the basic plumbing works: LocalStop calls result in
// stop messages being sent on the channel passed to WaitForStop.
func TestBasic(t *testing.T) {
	m, _ := rt.New(profileOpt)
	ch := make(chan string, 1)
	m.WaitForStop(ch)
	for i := 0; i < 10; i++ {
		m.Stop()
		if want, got := veyron2.LocalStop, <-ch; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
		select {
		case s := <-ch:
			t.Errorf("channel expected to be empty, got %q instead", s)
		default:
		}
	}
}

// TestMultipleWaiters verifies that the plumbing works with more than one
// registered wait channel.
func TestMultipleWaiters(t *testing.T) {
	m, _ := rt.New(profileOpt)
	ch1 := make(chan string, 1)
	m.WaitForStop(ch1)
	ch2 := make(chan string, 1)
	m.WaitForStop(ch2)
	for i := 0; i < 10; i++ {
		m.Stop()
		if want, got := veyron2.LocalStop, <-ch1; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
		if want, got := veyron2.LocalStop, <-ch2; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
	}
}

// TestMultipleStops verifies that LocalStop does not block even if the wait
// channel is not being drained: once the channel's buffer fills up, future
// Stops become no-ops.
func TestMultipleStops(t *testing.T) {
	m, _ := rt.New(profileOpt)
	ch := make(chan string, 1)
	m.WaitForStop(ch)
	for i := 0; i < 10; i++ {
		m.Stop()
	}
	if want, got := veyron2.LocalStop, <-ch; want != got {
		t.Errorf("WaitForStop want %q got %q", want, got)
	}
	select {
	case s := <-ch:
		t.Errorf("channel expected to be empty, got %q instead", s)
	default:
	}
}

func noWaiters(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	m, _ := rt.New(profileOpt)
	fmt.Fprintf(stdout, "ready\n")
	modules.WaitForEOF(stdin)
	m.Stop()
	os.Exit(42) // This should not be reached.
	return nil
}

// TestNoWaiters verifies that the child process exits in the absence of any
// wait channel being registered with its runtime.
func TestNoWaiters(t *testing.T) {
	sh := modules.NewShell(noWaitersCmd)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start(noWaitersCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	expect.NewSession(t, h.Stdout(), time.Minute).Expect("ready")
	want := fmt.Sprintf("exit status %d", veyron2.UnhandledStopExitCode)
	if err = h.Shutdown(os.Stderr, os.Stderr); err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

func forceStop(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	m, _ := rt.New(profileOpt)
	fmt.Fprintf(stdout, "ready\n")
	modules.WaitForEOF(stdin)
	m.WaitForStop(make(chan string, 1))
	m.ForceStop()
	os.Exit(42) // This should not be reached.
	return nil
}

// TestForceStop verifies that ForceStop causes the child process to exit
// immediately.
func TestForceStop(t *testing.T) {
	sh := modules.NewShell(forceStopCmd)
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start(forceStopCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("ready")
	err = h.Shutdown(os.Stderr, os.Stderr)
	want := fmt.Sprintf("exit status %d", veyron2.UnhandledStopExitCode)
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

func checkProgress(t *testing.T, ch <-chan veyron2.Task, progress, goal int) {
	if want, got := (veyron2.Task{progress, goal}), <-ch; !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected progress: want %+v, got %+v", want, got)
	}
}

func checkNoProgress(t *testing.T, ch <-chan veyron2.Task) {
	select {
	case p := <-ch:
		t.Errorf("channel expected to be empty, got %+v instead", p)
	default:
	}
}

// TestProgress verifies that the ticker update/track logic works for a single
// tracker.
func TestProgress(t *testing.T) {
	m, _ := rt.New(profileOpt)
	m.AdvanceGoal(50)
	ch := make(chan veyron2.Task, 1)
	m.TrackTask(ch)
	checkNoProgress(t, ch)
	m.AdvanceProgress(10)
	checkProgress(t, ch, 10, 50)
	checkNoProgress(t, ch)
	m.AdvanceProgress(5)
	checkProgress(t, ch, 15, 50)
	m.AdvanceGoal(50)
	checkProgress(t, ch, 15, 100)
	m.AdvanceProgress(1)
	checkProgress(t, ch, 16, 100)
	m.AdvanceGoal(10)
	checkProgress(t, ch, 16, 110)
	m.AdvanceProgress(-13)
	checkNoProgress(t, ch)
	m.AdvanceGoal(0)
	checkNoProgress(t, ch)
	m.Cleanup()
	if _, ok := <-ch; ok {
		t.Errorf("Expected channel to be closed")
	}
}

// TestProgressMultipleTrackers verifies that the ticker update/track logic
// works for more than one tracker.  It also ensures that the runtime doesn't
// block when the tracker channels are full.
func TestProgressMultipleTrackers(t *testing.T) {
	m, _ := rt.New(profileOpt)
	// ch1 is 1-buffered, ch2 is 2-buffered.
	ch1, ch2 := make(chan veyron2.Task, 1), make(chan veyron2.Task, 2)
	m.TrackTask(ch1)
	m.TrackTask(ch2)
	checkNoProgress(t, ch1)
	checkNoProgress(t, ch2)
	m.AdvanceProgress(1)
	checkProgress(t, ch1, 1, 0)
	checkNoProgress(t, ch1)
	checkProgress(t, ch2, 1, 0)
	checkNoProgress(t, ch2)
	for i := 0; i < 10; i++ {
		m.AdvanceProgress(1)
	}
	checkProgress(t, ch1, 2, 0)
	checkNoProgress(t, ch1)
	checkProgress(t, ch2, 2, 0)
	checkProgress(t, ch2, 3, 0)
	checkNoProgress(t, ch2)
	m.AdvanceGoal(4)
	checkProgress(t, ch1, 11, 4)
	checkProgress(t, ch2, 11, 4)
	m.Cleanup()
	if _, ok := <-ch1; ok {
		t.Errorf("Expected channel to be closed")
	}
	if _, ok := <-ch2; ok {
		t.Errorf("Expected channel to be closed")
	}
}

func app(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r, err := rt.New(profileOpt)
	if err != nil {
		return err
	}
	defer r.Cleanup()
	ch := make(chan string, 1)
	r.WaitForStop(ch)
	fmt.Fprintf(stdout, "Got %s\n", <-ch)
	r.AdvanceGoal(10)
	fmt.Fprintf(stdout, "Doing some work\n")
	r.AdvanceProgress(2)
	fmt.Fprintf(stdout, "Doing some more work\n")
	r.AdvanceProgress(5)
	return nil
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

func createConfigServer(t *testing.T, r veyron2.Runtime) (ipc.Server, string, <-chan string) {
	server, err := r.NewServer()
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	ch := make(chan string)

	var ep naming.Endpoint
	if ep, err = server.Listen(profiles.LocalListenSpec); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	if err := server.Serve("", node.ConfigServer(&configServer{ch}), vflag.NewAuthorizerOrDie()); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	return server, naming.JoinAddressName(ep.String(), ""), ch

}

func setupRemoteAppCycleMgr(t *testing.T) (veyron2.Runtime, modules.Handle, appcycle.AppCycleClientMethods, func()) {
	// We need to use the public API since stubs are used below (and they
	// refer to the global rt.R() function), but we take care to make sure
	// that the "google" runtime we are trying to test in this package is
	// the one being used.
	r, _ := rt.New(profileOpt)

	childcreds := security.NewVeyronCredentials(r.Principal(), appCmd)
	configServer, configServiceName, ch := createConfigServer(t, r)
	sh := modules.NewShell(appCmd)
	sh.SetVar(consts.VeyronCredentials, childcreds)
	sh.Config.Set(mgmt.ParentNameConfigKey, configServiceName)
	sh.Config.Set(mgmt.ProtocolConfigKey, "tcp")
	sh.Config.Set(mgmt.AddressConfigKey, "127.0.0.1:0")
	h, err := sh.Start("app", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	appCycleName := <-ch
	appCycle := appcycle.AppCycleClient(appCycleName)
	return r, h, appCycle, func() {
		configServer.Stop()
		sh.Cleanup(os.Stderr, os.Stderr)
		os.RemoveAll(childcreds)
		// Don't do r.Cleanup() since the runtime needs to be used by
		// more than one test case.
	}
}

// TestRemoteForceStop verifies that the child process exits when sending it
// a remote ForceStop rpc.
func TestRemoteForceStop(t *testing.T) {
	r, h, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	if err := appCycle.ForceStop(r.NewContext()); err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("Expected EOF error, got %v instead", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.ExpectEOF()
	err := h.Shutdown(os.Stderr, os.Stderr)
	want := fmt.Sprintf("exit status %d", veyron2.ForceStopExitCode)
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}

}

// TestRemoteStop verifies that the child shuts down cleanly when sending it
// a remote Stop rpc.
func TestRemoteStop(t *testing.T) {
	r, h, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	stream, err := appCycle.Stop(r.NewContext())
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	rStream := stream.RecvStream()
	expectTask := func(progress, goal int32) {
		if !rStream.Advance() {
			t.Fatalf("unexpected streaming error: %q", rStream.Err())
		}
		task := rStream.Value()
		if task.Progress != progress || task.Goal != goal {
			t.Errorf("Got (%d, %d), want (%d, %d)", task.Progress, task.Goal, progress, goal)
		}
	}
	expectTask(0, 10)
	expectTask(2, 10)
	expectTask(7, 10)
	if rStream.Advance() || rStream.Err() != nil {
		t.Errorf("Expected EOF, got (%v, %v) instead", rStream.Value(), rStream.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Got error %v", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect(fmt.Sprintf("Got %s", veyron2.RemoteStop))
	s.Expect("Doing some work")
	s.Expect("Doing some more work")
	s.ExpectEOF()
	if err := h.Shutdown(os.Stderr, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}
