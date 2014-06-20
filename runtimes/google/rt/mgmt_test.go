package rt_test

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"veyron2"
	"veyron2/ipc"
	"veyron2/mgmt"
	"veyron2/naming"
	"veyron2/services/mgmt/appcycle"

	"veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron/runtimes/google/rt"
	vflag "veyron/security/flag"
	"veyron/services/mgmt/node"
)

// TestBasic verifies that the basic plumbing works: LocalStop calls result in
// stop messages being sent on the channel passed to WaitForStop.
func TestBasic(t *testing.T) {
	m, _ := rt.New()
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
	m, _ := rt.New()
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
	m, _ := rt.New()
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

func init() {
	blackbox.CommandTable["noWaiters"] = noWaiters
}

func noWaiters([]string) {
	m, _ := rt.New()
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
	m.Stop()
	os.Exit(42) // This should not be reached.
}

// TestNoWaiters verifies that the child process exits in the absence of any
// wait channel being registered with its runtime.
func TestNoWaiters(t *testing.T) {
	c := blackbox.HelperCommand(t, "noWaiters")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	c.CloseStdin()
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", veyron2.UnhandledStopExitCode))
}

func init() {
	blackbox.CommandTable["forceStop"] = forceStop
}

func forceStop([]string) {
	m, _ := rt.New()
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
	m.WaitForStop(make(chan string, 1))
	m.ForceStop()
	os.Exit(42) // This should not be reached.
}

// TestForceStop verifies that ForceStop causes the child process to exit
// immediately.
func TestForceStop(t *testing.T) {
	c := blackbox.HelperCommand(t, "forceStop")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	c.CloseStdin()
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", veyron2.ForceStopExitCode))
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
	m, _ := rt.New()
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
	m.Shutdown()
	if _, ok := <-ch; ok {
		t.Errorf("Expected channel to be closed")
	}
}

// TestProgressMultipleTrackers verifies that the ticker update/track logic
// works for more than one tracker.  It also ensures that the runtime doesn't
// block when the tracker channels are full.
func TestProgressMultipleTrackers(t *testing.T) {
	m, _ := rt.New()
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
	m.Shutdown()
	if _, ok := <-ch1; ok {
		t.Errorf("Expected channel to be closed")
	}
	if _, ok := <-ch2; ok {
		t.Errorf("Expected channel to be closed")
	}
}

func init() {
	blackbox.CommandTable["app"] = app
}

func app([]string) {
	r, err := rt.New()
	if err != nil {
		fmt.Printf("Error creating runtime: %v\n", err)
		return
	}
	defer r.Shutdown()
	ch := make(chan string, 1)
	r.WaitForStop(ch)
	fmt.Printf("Got %s\n", <-ch)
	r.AdvanceGoal(10)
	fmt.Println("Doing some work")
	r.AdvanceProgress(2)
	fmt.Println("Doing some more work")
	r.AdvanceProgress(5)
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
	const suffix = ""
	ch := make(chan string)
	if err := server.Register(suffix, ipc.SoloDispatcher(node.NewServerConfig(&configServer{ch}), vflag.NewAuthorizerOrDie())); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	var ep naming.Endpoint
	if ep, err = server.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	return server, naming.JoinAddressName(ep.String(), suffix), ch

}

func setupRemoteAppCycleMgr(t *testing.T) (*blackbox.Child, appcycle.AppCycle, func()) {
	r, err := rt.Init()
	if err != nil {
		t.Fatalf("Error creating runtime: %v", err)
	}
	c := blackbox.HelperCommand(t, "app")
	id := r.Identity()
	idFile := testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(id, "test"))
	configServer, configServiceName, ch := createConfigServer(t)
	c.Cmd.Env = append(c.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile),
		fmt.Sprintf("%v=%v", mgmt.ParentNodeManagerConfigKey, configServiceName))
	c.Cmd.Start()
	appCycleName := <-ch
	appCycle, err := appcycle.BindAppCycle(appCycleName)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	return c, appCycle, func() {
		configServer.Stop()
		c.Cleanup()
		os.Remove(idFile)
		// Don't do r.Shutdown() since the runtime needs to be used by
		// more than one test case.
	}
}

// TestRemoteForceStop verifies that the child process exits when sending it
// a remote ForceStop rpc.
func TestRemoteForceStop(t *testing.T) {
	c, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	if err := appCycle.ForceStop(rt.R().NewContext()); err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("Expected EOF error, got %v instead", err)
	}
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", veyron2.ForceStopExitCode))
}

// TestRemoteStop verifies that the child shuts down cleanly when sending it
// a remote Stop rpc.
func TestRemoteStop(t *testing.T) {
	c, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	stream, err := appCycle.Stop(rt.R().NewContext())
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	expectTask := func(progress, goal int32) {
		if task, err := stream.Recv(); err != nil {
			t.Fatalf("unexpected streaming error: %q", err)
		} else if task.Progress != progress || task.Goal != goal {
			t.Errorf("Got (%d, %d), want (%d, %d)", task.Progress, task.Goal, progress, goal)
		}
	}
	expectTask(0, 10)
	expectTask(2, 10)
	expectTask(7, 10)
	if task, err := stream.Recv(); err != io.EOF {
		t.Errorf("Expected (nil, EOF), got (%v, %v) instead", task, err)
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Got error %v", err)
	}
	c.Expect(fmt.Sprintf("Got %s", veyron2.RemoteStop))
	c.Expect("Doing some work")
	c.Expect("Doing some more work")
	c.ExpectEOFAndWait()
}
