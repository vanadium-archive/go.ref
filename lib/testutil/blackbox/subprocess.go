package blackbox

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"veyron2/vlog"
)

type mainFunc func(argv []string)

type commandMap map[string]mainFunc

var (
	CommandTable = make(commandMap)
	subcommand   string
)

func init() {
	flag.StringVar(&subcommand, "subcommand", "", "the subcommand to run")
}

// Child represents a child process.
type Child struct {
	Name      string
	Cmd       *exec.Cmd
	ioLock    sync.Mutex
	Stdout    *bufio.Reader // GUARDED_BY(ioLock)
	Stdin     io.WriteCloser
	stderr    *os.File
	t         *testing.T
	hasFailed bool
}

func testTimeout() string {
	if timeout := flag.Lookup("test.timeout"); timeout != nil {
		val := timeout.Value.(flag.Getter).Get().(time.Duration)
		if val.String() != timeout.DefValue {
			return "--test.timeout=" + timeout.Value.String()
		}
	}
	return "--test.timeout=1m"
}

// HelperCommand() takes an argument list and starts a helper subprocess.
// t maybe nil to allow use from outside of tests.
func HelperCommand(t *testing.T, command string, args ...string) *Child {
	cs := []string{"--subcommand=" + command}
	if t != nil {
		cs = append(cs, "-test.run=TestHelperProcess")
	}
	// If timeout is not specified on the command line set a
	// default value.
	cs = append(cs, testTimeout())
	for fname, fval := range vlog.Log.ExplicitlySetFlags() {
		cs = append(cs, fmt.Sprintf("--%s=%s", fname, fval))
	}
	cs = append(cs, args...)
	vlog.VI(2).Infof("running: %s %s", os.Args[0], cs)
	cmd := exec.Command(os.Args[0], cs...)
	stderr, err := ioutil.TempFile("", "__test__"+strings.TrimLeft(command, "-\n\t "))
	if err != nil {
		vlog.Errorf("Failed to open temp file, using stderr instead: err %s", err)
		cmd.Stderr = os.Stderr
		stderr = nil
	} else {
		cmd.Stderr = stderr
	}
	cmd.Env = append([]string{"VEYRON_BLACKBOX_TEST=1"}, os.Environ()...)
	stdout, _ := cmd.StdoutPipe()
	stdin, _ := cmd.StdinPipe()
	return &Child{
		Cmd:    cmd,
		Name:   command,
		Stdout: bufio.NewReader(stdout),
		Stdin:  stdin,
		stderr: stderr,
		t:         t,
		hasFailed: false,
	}
}

// HelperProcess is the entry point for helper subprocesses.
// Children should Write errors to stderr and test output to stdout since
// the parent process will read from stdout a line at a time to monitor
// the progress of the child.
// t maybe nil to allow use from outside of tests.
func HelperProcess(t *testing.T) {
	// Return immediately if this is not run as the child helper
	// for the other tests.
	if os.Getenv("VEYRON_BLACKBOX_TEST") != "1" {
		return
	}
	if len(subcommand) == 0 {
		vlog.Fatalf("No command found in: %s", os.Args)
	}
	mainFunc, ok := CommandTable[subcommand]
	if !ok {
		vlog.Fatalf("Unknown cmd: '%s'", subcommand)
	}

	go func(pid int) {
		for {
			_, err := os.FindProcess(pid)
			if err != nil {
				vlog.Fatalf("Looks like our parent exited: %v", err)
			}
			time.Sleep(time.Second)
		}
	}(os.Getppid())

	mainFunc(flag.Args())
	os.Exit(0)
}

type Reader func(r *bufio.Reader) (string, error)

// ReadAll will read up to 16K of data from a buffered I/O reader, it is
// intended to be used with ReadWithTimeout.
func ReadAll(r *bufio.Reader) (string, error) {
	buf := make([]byte, 4096*4)
	n, err := r.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// ReadLine will read a single line from a buffered I/O reader, it is
// intended to be used with ReadWithTimeout.
func ReadLine(r *bufio.Reader) (string, error) {
	return r.ReadString('\n')
}

func (c *Child) failed() bool {
	if c.t != nil {
		return c.t.Failed()
	}
	return c.hasFailed
}
func (c *Child) error(m string) {
	c.hasFailed = true
	if c.t != nil {
		c.t.Error(m)
	} else {
		fmt.Fprintln(os.Stderr, m)
	}
}

// ReadWithTimeout reads data from bufio.Reader instance using a function
// of type Reader such as ReadAll or ReadLine. It will return true if it
// times out, false otherwise. It will terminate the Child if it encounters
// a timeout.
func (c *Child) ReadWithTimeout(f Reader, r *bufio.Reader, timeout time.Duration) (string, bool, error) {
	ch := make(chan string, 1)
	ech := make(chan error, 1)
	go func(c *Child) {
		c.ioLock.Lock()
		s, err := f(r)
		c.ioLock.Unlock()
		if err != nil {
			if err != io.EOF {
				vlog.VI(2).Info(formatLogLine("failed to read message: error %s: '%s'", err, s))
			}
			ech <- err
			return
		}
		ch <- s
	}(c)
	select {
	case err := <-ech:
		return "", false, err
	case m := <-ch:
		return strings.TrimRight(m, "\n"), false, nil
	case <-time.After(timeout):
		// Kill the sub process to get the read calls running
		// in the goroutine above to return an err.
		c.Cmd.Process.Kill()
		return "", true, nil
	}
	panic("unreachable")
}

func (c *Child) readRemaining(stream string, r *bufio.Reader) {
	text, timedout, err := c.ReadWithTimeout(ReadAll, r, waitSeconds)
	switch {
	case timedout:
		c.error(formatLogLine("%s: timedout reading %s: err '%v'", c.Name, stream, err))
	case err != nil:
		c.error(formatLogLine("%s: err reading %s", c.Name, stream))
	default:
		vlog.Info(formatLogLine("%s: %s: '%s'", c.Name, stream, text))
	}
}

func (c *Child) expectLine(expected string) bool {
	actual, timedout, err := c.ReadWithTimeout(ReadLine, c.Stdout, waitSeconds)
	switch {
	case timedout:
		c.error(formatLogLine("%s: timedout reading from stdout, expecting '%s'", c.Name, expected))
		return false
	case err != nil && err != io.EOF:
		c.error(formatLogLine("%s: unexpected error: %s, expecting '%s'", c.Name, err, expected))
		return false
	case actual == expected:
		vlog.VI(2).Info(formatLogLine("%s: got: '%s'", c.Name, strings.TrimRight(actual, "")))
		return err == io.EOF
	default:
		c.error(formatLogLine("%s: expected '%s': got '%s'", c.Name, expected, actual))
		return err == io.EOF
	}
}

// removeIfFound removes the give item from the given list, if present
// (returning if the item was present).  If several copies of the item exist,
// removeIfFound only removes the first occurrence.
func removeIfFound(item string, list *[]string) bool {
	for i, e := range *list {
		if e == item {
			*list = append((*list)[0:i], (*list)[i+1:len(*list)]...)
			return true
		}
	}
	return false
}

func (c *Child) expectSetEventually(expected []string, timeout time.Duration) bool {
	for {
		actual, timedout, err := c.ReadWithTimeout(ReadLine, c.Stdout, timeout)
		switch {
		case timedout:
			m := formatLogLine("%s: timedout reading from stdout", c.Name)
			vlog.VI(2).Info(m)
			c.error(m)
			return false
		case err != nil && err != io.EOF:
			m := formatLogLine("%s: unexpected error: %s", c.Name, err)
			vlog.VI(2).Info(m)
			c.error(m)
			return false
		case removeIfFound(actual, &expected):
			if len(expected) == 0 {
				return err == io.EOF
			}
		case err == io.EOF:
			m := formatLogLine("%s: eof", c.Name)
			vlog.VI(2).Info(m)
			c.error(m)
			return true
		default:
			vlog.VI(2).Info(formatLogLine("%s: got: '%s'", c.Name, strings.TrimRight(actual, "\n")))
		}
	}
}

// Expect the specified string to be read from the Child's stdout pipe.
// Trailing \n's are stripped from the Child's output and hence should not
// be included in the "expected" parameter.
func (c *Child) Expect(expected string) {
	if c.failed() {
		vlog.VI(2).Info(formatLogLine("Already failed"))
		return
	}
	eof := c.expectLine(expected)
	if eof {
		c.error(formatLogLine("%s: unexpected EOF when expecting '%s'", c.Name, strings.TrimRight(expected, "\n")))
	}
}

// ExpectSet verifies whether the given set of strings (expressed as a list,
// though order is irrelevant) matches the next len(expected) lines in stdout.
// The set is allowed to contain repetitions if the same line is expected
// multiple times.
func (c *Child) ExpectSet(expected []string) {
	if c.failed() {
		return
	}
	actual := make([]string, 0, len(expected))
	for i := 0; i < len(expected); i++ {
		str, err := c.ReadLineFromChild()
		if err != nil {
			c.error(formatLogLine("ReadLineFromChild: failed %v", err))
			return
		}
		actual = append(actual, str)
	}
	sort.Strings(expected)
	sort.Strings(actual)
	for i, exp := range expected {
		if exp != actual[i] {
			c.error(formatLogLine("expected %v, actual %v", expected, actual))
			return
		}
	}
}

// Expect the specified string to be read from the Child's stdout pipe
// eventually.  There may be additional output beforehand.
func (c *Child) ExpectEventually(expected string, timeout time.Duration) {
	if c.failed() {
		vlog.VI(2).Info(formatLogLine("Already failed"))
		return
	}
	vlog.VI(2).Info(formatLogLine("Waiting for client, timeout %s", timeout))
	eof := c.expectSetEventually([]string{expected}, timeout)
	if eof {
		c.error(formatLogLine("%s: unexpected EOF when expecting %q", c.Name, strings.TrimRight(expected, "\n")))
	}
}

// ExpectSetEventually verifies whether the given set of strings (expressed as a
// list, though order is irrelevant) appear in the stdout pipe of the child
// eventually.  The set is allowed to contain repetitions if the same line is
// expected multiple times.
func (c *Child) ExpectSetEventually(expected []string, timeout time.Duration) {
	if c.failed() {
		vlog.VI(2).Info(formatLogLine("Already failed"))
		return
	}
	vlog.VI(2).Info(formatLogLine("Waiting for client, timeout %s", timeout))
	eof := c.expectSetEventually(expected, timeout)
	if eof {
		c.error(formatLogLine("%s: unexpected EOF when expecting %q", c.Name, expected))
	}
}

// Expect EOF to be read from the Child's stdout pipe and wait for the child
// if it is still running to clean up process state.
func (c *Child) ExpectEOFAndWait() {
	c.ExpectEOFAndWaitForExitCode(nil)
}

// ExpectEOFAndWaitForExitCode is the same as ExpectEOFAndWait except that it
// allows for a non-nil error to be returned by the child.
func (c *Child) ExpectEOFAndWaitForExitCode(exit error) {
	if c.failed() {
		c.error(formatLogLine("%s: cleanup", c.Name))
		c.readRemaining("stdout", c.Stdout)
		if c.Cmd.Process != nil {
			c.Cmd.Process.Kill()
		}
	} else {
		eof := c.expectLine("")
		if !eof {
			c.error(formatLogLine("%s: failed to exit (failed to read EOF)", c.Name))
			c.readRemaining("stdout", c.Stdout)
			if c.Cmd.Process != nil {
				c.Cmd.Process.Kill()
			}
		}
	}
	c.ioLock.Lock()
	defer c.ioLock.Unlock()
	err := c.Cmd.Wait()
	if exit == nil {
		if err != nil {
			c.error(formatLogLine("Client exited with error: %s", err))
		}
		return
	}
	if err == nil {
		c.error(formatLogLine("Client exited without error: expecting %s", exit))
		return
	}
	if err.Error() != exit.Error() {
		c.error(formatLogLine("Client exited with unexpected error: %q and not %q", err, exit))
	}
}

// The WaitForXXX() functions are similar to expectations, but they do less
// checking and for use in benchmarking.

// WaitForLine reads the input until the expected line is read,
// returning an error if EOF is reached or there is a timeout.
func (c *Child) WaitForLine(expected string, timeout time.Duration) error {
	if c.failed() {
		vlog.VI(2).Info(formatLogLine("Already failed"))
		return fmt.Errorf("Already failed")
	}
	deadline := time.Now().Add(timeout)
	for {
		actual, _, err := c.ReadWithTimeout(ReadLine, c.Stdout, deadline.Sub(time.Now()))
		if err != nil {
			return err
		}
		if actual == expected {
			return nil
		}
	}
}

// WaitForEOF reads the input until an EOF is reached, returning an
// error if there is a timeout.
func (c *Child) WaitForEOF(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, _, err := c.ReadWithTimeout(ReadLine, c.Stdout, deadline.Sub(time.Now()))
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// WaitForPortFromChild waits for a line in the format "port %d" and returns the
// port number, or an error if there is a timeout or EOF is reached.
func (c *Child) WaitForPortFromChild() (string, error) {
	actual, _, err := c.ReadWithTimeout(ReadLine, c.Stdout, waitSeconds)
	if err != nil {
		return "", err
	}
	last := strings.LastIndex(actual, ":")
	if last == -1 {
		return "", fmt.Errorf("%s", formatLogLine("%s: failed to parse port from %q - missing ':'", c.Name, actual))
	}
	port := actual[last+1:]
	port = strings.TrimSuffix(port, "'\n")
	// Check that the port is a number.
	_, err = strconv.Atoi(port)
	if err != nil {
		return "", fmt.Errorf("%s", formatLogLine("%s: failed to parse port from %q: err %s", c.Name, actual, err))
	}
	return port, nil
}

// ReadPortFromChild reads a port number written to the child's stdout,
// returning an error if it fails to do so and not calling t.Error internal.
func (c *Child) ReadPortFromChild() (string, error) {
	if c.failed() {
		return "", fmt.Errorf("%s", "Already failed")
	}
	actual, timedout, err := c.ReadWithTimeout(ReadLine, c.Stdout, waitSeconds)
	if timedout {
		return "", fmt.Errorf("%s", formatLogLine("%s: timeout reading port", c.Name))
	}
	if err != nil {
		return "", fmt.Errorf("%s", formatLogLine("%s: error reading port: %s", c.Name, err))
	}
	last := strings.LastIndex(actual, ":")
	if last == -1 {
		return "", fmt.Errorf("%s", formatLogLine("%s: failed to parse port from '%s' - missing ':'", c.Name, actual))
	}
	port := actual[last+1:]
	port = strings.TrimRight(port, "'\n")
	_, err = strconv.Atoi(port)
	if err != nil {
		return "", fmt.Errorf("%s", formatLogLine("%s: failed to parse port from '%s': err %s", c.Name, actual, err))
	}
	return port, nil
}

// ReadLineFromChild reads a single line from the child's stdout,
// returning an error if it fails to do so and not calling t.Error internal.
func (c *Child) ReadLineFromChild() (string, error) {
	if c.failed() {
		return "", fmt.Errorf("Already failed")
	}
	actual, timedout, err := c.ReadWithTimeout(ReadLine, c.Stdout, waitSeconds)
	if timedout {
		return "", fmt.Errorf("%s", formatLogLine("%s: timeout reading line", c.Name))
	}
	if err != nil {
		return "", fmt.Errorf("%s", formatLogLine("%s: error reading line: %s", c.Name, err))
	}
	return strings.TrimRight(actual, "\n"), nil
}

// WriteLine writes the given line to the child's stdin, and appends a \n.
func (c *Child) WriteLine(line string) {
	if c.failed() {
		vlog.VI(2).Info(formatLogLine("Already failed"))
		return
	}
	c.Stdin.Write([]byte(line))
	c.Stdin.Write([]byte("\n"))
}

// CloseStding closes the stdin pipe the child is likely blocked on.
func (c *Child) CloseStdin() {
	// Child should exit now.
	c.Stdin.Close()
}

// Cleanup sends a kill signal to the child, prints any stderr logs from
// the child and deletes the log files generated by the child.
func (c *Child) Cleanup() {
	vlog.FlushLog()
	if c.Cmd.Process != nil {
		c.Cmd.Process.Kill()
	}
	if c.stderr != nil {
		defer func() {
			c.stderr.Close()
			os.Remove(c.stderr.Name())
		}()
		if _, err := c.stderr.Seek(0, 0); err != nil {
			return
		}
		scanner := bufio.NewScanner(c.stderr)
		for scanner.Scan() {
			// Avoid printing two sets of line headers
			vlog.Info(c.Name + ": " + scanner.Text())
		}
	}

}
