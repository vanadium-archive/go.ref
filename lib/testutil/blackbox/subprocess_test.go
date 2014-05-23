package blackbox_test

// TODO(cnicolaou): add tests for error cases that result in t.Fatalf being
// called. We need to mock testing.T in order to be able to catch these.

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
)

func isFatalf(t *testing.T, err error, format string, args ...interface{}) {
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, format, args...))
	}
}

var globalT *testing.T

func TestHelperProcess(t *testing.T) {
	globalT = t
	blackbox.HelperProcess(t)
}

func finish(c *blackbox.Child) {
	c.CloseStdin()
	c.Expect("done")
	c.ExpectEOFAndWait()
}

func TestExpect(t *testing.T) {
	c := blackbox.HelperCommand(t, "print", "a", "b", "c")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("a")
	c.Expect("b")
	c.Expect("c")
	finish(c)
}

func TestExpectSet(t *testing.T) {
	c := blackbox.HelperCommand(t, "print", "a", "b", "c")
	defer c.Cleanup()
	c.Cmd.Start()
	c.ExpectSet([]string{"c", "b", "a"})
	finish(c)
}

func TestExpectEventually(t *testing.T) {
	c := blackbox.HelperCommand(t, "print", "a", "b", "c")
	defer c.Cleanup()
	c.Cmd.Start()
	c.ExpectEventually("c", time.Second)
	finish(c)
}

func TestExpectSetEventually(t *testing.T) {
	c := blackbox.HelperCommand(t, "print", "a", "b", "c", "d", "b")
	defer c.Cleanup()
	c.Cmd.Start()
	c.ExpectSetEventually([]string{"d", "b", "b"}, time.Second)
	finish(c)
}

func TestReadPort(t *testing.T) {
	c := blackbox.HelperCommand(t, "print", "host:1234")
	defer c.Cleanup()
	c.Cmd.Start()
	port, err := c.ReadPortFromChild()
	if err != nil {
		t.Fatalf("failed to read port from child: %s", err)
	}
	if port != "1234" {
		t.Fatalf("unexpected port from child: %q", port)
	}
	finish(c)
}

func TestEcho(t *testing.T) {
	c := blackbox.HelperCommand(t, "echo")
	defer c.Cleanup()
	c.Cmd.Start()
	c.WriteLine("a")
	c.WriteLine("b")
	c.WriteLine("c")
	c.ExpectEventually("c", time.Second)
	finish(c)
}

func TestTimeout(t *testing.T) {
	c := blackbox.HelperCommand(t, "echo")
	defer c.Cleanup()
	c.Cmd.Start()
	c.WriteLine("a")
	l, timeout, err := c.ReadWithTimeout(blackbox.ReadLine, c.Stdout, time.Second)
	isFatalf(t, err, "unexpected error: %s", err)
	if timeout {
		t.Fatalf("unexpected timeout")
	}
	if l != "a" {
		t.Fatalf("unexpected input: got %q", l)
	}
	_, timeout, _ = c.ReadWithTimeout(blackbox.ReadLine, c.Stdout, 250*time.Millisecond)
	if !timeout {
		t.Fatalf("failed to timeout")
	}
	_, _, err = c.ReadWithTimeout(blackbox.ReadLine, c.Stdout, 250*time.Millisecond)
	// The previous call to ReadWithTimeout which timed out kills the
	// Child, so a subsequent call will read an EOF.
	if err != io.EOF {
		t.Fatalf("expected EOF: got %q instead", err)
	}
}

func TestWait(t *testing.T) {
	c := blackbox.HelperCommand(t, "print", "a", "b:1234")
	defer c.Cleanup()
	c.Cmd.Start()
	err := c.WaitForLine("a", time.Second)
	isFatalf(t, err, "unexpected error: %s", err)
	port, err := c.WaitForPortFromChild()
	isFatalf(t, err, "unexpected error: %s", err)
	if port != "1234" {
		isFatalf(t, err, "unexpected port: %s", port)
	}
	c.CloseStdin()
	c.WaitForEOF(time.Second)
}

func init() {
	blackbox.CommandTable["sleepChild"] = sleepChild
	blackbox.CommandTable["sleepParent"] = sleepParent
}

// SleepChild spleeps essentially forever, this process should only
// die when its parent dies and it will exit via the
// blackbox framework and not here.
func sleepChild(args []string) {
	fmt.Printf("%d\n", os.Getpid())
	time.Sleep(100 * time.Hour)
	panic("this test should never be allowed to run this long!")
}

// Spawn the sleepChild process above.
func spawnSleeper() *blackbox.Child {
	c := blackbox.HelperCommand(globalT, "sleepChild")
	c.Cmd.Start()
	fmt.Println("ready")
	line, err := c.ReadLineFromChild()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unexpected error: %v", err)
		os.Exit(1)
	}
	// Print out the pid of the sleepChild first and ourselves second
	fmt.Println(line)
	fmt.Printf("%d\n", os.Getpid())
	return c
}

// Spawn a sleeper and wait for our parent to tell us when to exit
func sleepParent(args []string) {
	c := spawnSleeper()
	defer c.Cleanup()
	blackbox.WaitForEOFOnStdin()
}

// Read the pids from the child processes and make sure they exist
func readAndTestPids(t *testing.T, c *blackbox.Child) (child, parent int) {
	// Read the pids of the sleeper and the process waiting on it
	line, err := c.ReadLineFromChild()
	isFatalf(t, err, "unexpected error: %v", err)
	child, err = strconv.Atoi(strings.TrimRight(line, "\n"))
	isFatalf(t, err, "unexpected error: %v", err)
	line, err = c.ReadLineFromChild()
	isFatalf(t, err, "unexpected error: %v", err)
	parent, err = strconv.Atoi(strings.TrimRight(line, "\n"))
	isFatalf(t, err, "unexpected error: %v", err)

	// Make sure both subprocs exist
	err = syscall.Kill(child, 0)
	isFatalf(t, err, "unexpected error: %v", err)
	err = syscall.Kill(parent, 0)
	isFatalf(t, err, "unexpected error: %v", err)

	return child, parent
}

// Wait for the specified pids to no longer exist
func waitForNonExistence(t *testing.T, pids []int) {
	w := make(chan struct{})
	go func() {
		for {
			done := 0
			for _, pid := range pids {
				err := syscall.Kill(pid, 0)
				if err == nil {
					continue
				}
				done++
			}
			if done == len(pids) {
				w <- struct{}{}
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()

	select {
	case <-w:
	case <-time.After(10 * time.Second):
		t.Errorf("timed out on children")
	}
}

func TestCloser(t *testing.T) {
	c := blackbox.HelperCommand(t, "sleepParent")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")

	sleeper, parent := readAndTestPids(t, c)

	// Ask the sleep waiter to terminate and wait for it to do so.
	c.CloseStdin()
	c.ExpectEOFAndWait()

	waitForNonExistence(t, []int{sleeper, parent})
}

func TestSubtimeout(t *testing.T) {
	c := blackbox.HelperCommand(t, "sleepChild", "--test.timeout=2s")
	defer c.Cleanup()
	c.Cmd.Start()

	line, err := c.ReadLineFromChild()
	isFatalf(t, err, "unexpected error: %v", err)
	child, err := strconv.Atoi(strings.TrimRight(line, "\n"))
	err = syscall.Kill(child, 0)
	isFatalf(t, err, "unexpected error: %v", err)

	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status 2"))

	waitForNonExistence(t, []int{child})
}
