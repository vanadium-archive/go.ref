package mgmt

import (
	"fmt"
	"os"
	"testing"

	"veyron2/mgmt"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
)

// TestHelperProcess is boilerplate for the blackbox setup.
func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

// TestBasic verifies that the basic plumbing works: LocalStop calls result in
// stop messages being sent on the channel passed to WaitForStop.
func TestBasic(t *testing.T) {
	m := New()
	ch := make(chan string, 1)
	m.WaitForStop(ch)
	for i := 0; i < 10; i++ {
		m.Stop()
		if want, got := mgmt.LocalStop, <-ch; want != got {
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
	m := New()
	ch1 := make(chan string, 1)
	m.WaitForStop(ch1)
	ch2 := make(chan string, 1)
	m.WaitForStop(ch2)
	for i := 0; i < 10; i++ {
		m.Stop()
		if want, got := mgmt.LocalStop, <-ch1; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
		if want, got := mgmt.LocalStop, <-ch2; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
	}
}

// TestMultipleStops verifies that LocalStop does not block even if the wait
// channel is not being drained: once the channel's buffer fills up, future
// Stops become no-ops.
func TestMultipleStops(t *testing.T) {
	m := New()
	ch := make(chan string, 1)
	m.WaitForStop(ch)
	for i := 0; i < 10; i++ {
		m.Stop()
	}
	if want, got := mgmt.LocalStop, <-ch; want != got {
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
	m := New()
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
	m.Stop()
	os.Exit(42) // This should not be reached.
}

// TestNoWaiters verifies that the child process exits in the absense of any
// wait channel being registered with its runtime.
func TestNoWaiters(t *testing.T) {
	c := blackbox.HelperCommand(t, "noWaiters")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	c.CloseStdin()
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", mgmt.UnhandledStopExitCode))
}

func init() {
	blackbox.CommandTable["forceStop"] = forceStop
}

func forceStop([]string) {
	m := New()
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
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status %d", mgmt.ForceStopExitCode))
}
