package blackbox_test

// TODO(cnicolaou): add tests for error cases that result in t.Fatalf being
// called. We need to mock testing.T in order to be able to catch these.

import (
	"io"
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

func TestHelperProcess(t *testing.T) {
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
