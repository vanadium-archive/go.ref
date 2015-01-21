package rt_test

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/appcycle"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/profiles"
	vflag "v.io/core/veyron/security/flag"
	"v.io/core/veyron/services/mgmt/device"
)

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
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	m := veyron2.GetAppCycle(ctx)
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	m := veyron2.GetAppCycle(ctx)
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	m := veyron2.GetAppCycle(ctx)
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	m := veyron2.GetAppCycle(ctx)
	fmt.Fprintf(stdout, "ready\n")
	modules.WaitForEOF(stdin)
	m.Stop()
	os.Exit(42) // This should not be reached.
	return nil
}

// TestNoWaiters verifies that the child process exits in the absence of any
// wait channel being registered with its runtime.
func TestNoWaiters(t *testing.T) {
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	m := veyron2.GetAppCycle(ctx)
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
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
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

func checkProgress(t *testing.T, ch <-chan veyron2.Task, progress, goal int32) {
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
	ctx, shutdown := veyron2.Init()

	m := veyron2.GetAppCycle(ctx)
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
	shutdown()
	if _, ok := <-ch; ok {
		t.Errorf("Expected channel to be closed")
	}
}

// TestProgressMultipleTrackers verifies that the ticker update/track logic
// works for more than one tracker.  It also ensures that the runtime doesn't
// block when the tracker channels are full.
func TestProgressMultipleTrackers(t *testing.T) {
	ctx, shutdown := veyron2.Init()

	m := veyron2.GetAppCycle(ctx)
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
	shutdown()
	if _, ok := <-ch1; ok {
		t.Errorf("Expected channel to be closed")
	}
	if _, ok := <-ch2; ok {
		t.Errorf("Expected channel to be closed")
	}
}

func app(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	m := veyron2.GetAppCycle(ctx)
	ch := make(chan string, 1)
	m.WaitForStop(ch)
	fmt.Fprintf(stdout, "Got %s\n", <-ch)
	m.AdvanceGoal(10)
	fmt.Fprintf(stdout, "Doing some work\n")
	m.AdvanceProgress(2)
	fmt.Fprintf(stdout, "Doing some more work\n")
	m.AdvanceProgress(5)
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

func createConfigServer(t *testing.T, ctx *context.T) (ipc.Server, string, <-chan string) {
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	ch := make(chan string)
	var eps []naming.Endpoint
	if eps, err = server.Listen(profiles.LocalListenSpec); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	if err := server.Serve("", device.ConfigServer(&configServer{ch}), vflag.NewAuthorizerOrDie()); err != nil {
		t.Fatalf("Got error: %v", err)
	}
	return server, eps[0].Name(), ch
}

func setupRemoteAppCycleMgr(t *testing.T) (*context.T, modules.Handle, appcycle.AppCycleClientMethods, func()) {
	ctx, shutdown := veyron2.Init()

	configServer, configServiceName, ch := createConfigServer(t, ctx)
	sh, err := modules.NewShell(ctx, veyron2.GetPrincipal(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	sh.SetConfigKey(mgmt.ParentNameConfigKey, configServiceName)
	sh.SetConfigKey(mgmt.ProtocolConfigKey, "tcp")
	sh.SetConfigKey(mgmt.AddressConfigKey, "127.0.0.1:0")
	h, err := sh.Start("app", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	appCycleName := ""
	select {
	case appCycleName = <-ch:
	case <-time.After(time.Minute):
		t.Errorf("timeout")
	}
	appCycle := appcycle.AppCycleClient(appCycleName)
	return ctx, h, appCycle, func() {
		configServer.Stop()
		sh.Cleanup(os.Stderr, os.Stderr)
		shutdown()
	}
}

// TestRemoteForceStop verifies that the child process exits when sending it
// a remote ForceStop rpc.
func TestRemoteForceStop(t *testing.T) {
	ctx, h, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	if err := appCycle.ForceStop(ctx); err == nil || !strings.Contains(err.Error(), "EOF") {
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
	ctx, h, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	stream, err := appCycle.Stop(ctx)
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
