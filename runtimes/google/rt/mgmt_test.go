package rt_test

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"veyron2"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron/runtimes/google/rt"
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
}
