// Package expect provides support for testing the contents from a buffered
// input stream. It supports literal and pattern based matching. It is
// line oriented; all of the methods (expect ReadAll) strip trailing newlines
// from their return values. It places a timeout on all its operations.
// It will generally be used to read from the stdout stream of subprocesses
// in tests and other situations and to make 'assertions'
// about what is to be read.
//
// A Session type is used to store state, in particular error state, across
// consecutive invocations of its method set. If a particular method call
// encounters an error then subsequent calls on that Session will have no
// effect. This allows for a series of assertions to be made, one per line,
// and for errors to be checked at the end. In addition Session is designed
// to be easily used with the testing package; passing a testing.T instance
// to NewSession allows it to set errors directly and hence tests will pass or
// fail according to whether the expect assertions are met or not.
//
// Care is taken to ensure that the file and line number of the first
// failed assertion in the session are recorded in the error stored in
// the Session.
//
// Examples
//
// func TestSomething(t *testing.T) {
//     buf := []byte{}
//     buffer := bytes.NewBuffer(buf)
//     buffer.WriteString("foo\n")
//     buffer.WriteString("bar\n")
//     buffer.WriteString("baz\n")
//     s := expect.New(t, bufio.NewReader(buffer), time.Second)
//     s.Expect("foo")
//     s.Expect("bars)
//     if got, want := s.ReadLine(), "baz"; got != want {
//         t.Errorf("got %v, want %v", got, want)
//     }
// }
//
package expect

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"veyron.io/veyron/veyron2/vlog"
)

var (
	Timeout = errors.New("timeout")
)

// Session represents the state of an expect session.
type Session struct {
	input   *bufio.Reader
	timeout time.Duration
	t       Testing
	verbose bool
	err     error
}

type Testing interface {
	Error(args ...interface{})
	Log(args ...interface{})
}

// NewSession creates a new Session. The parameter t may be safely be nil.
func NewSession(t Testing, input io.Reader, timeout time.Duration) *Session {
	return &Session{t: t, timeout: timeout, input: bufio.NewReader(input)}
}

// Failed returns true if an error has been encountered by a prior call.
func (s *Session) Failed() bool {
	return s.err != nil
}

// Error returns the error code (possibly nil) currently stored in the Session.
func (s *Session) Error() error {
	return s.err
}

// SetVerbosity enables/disable verbose debugging information, in particular,
// every line of input read will be logged via Testing.Logf or, if it is nil,
// to stderr.
func (s *Session) SetVerbosity(v bool) {
	s.verbose = v
}

func (s *Session) log(format string, args ...interface{}) {
	if !s.verbose {
		return
	}
	_, path, line, _ := runtime.Caller(2)
	loc := fmt.Sprintf("%s:%d", filepath.Base(path), line)
	o := strings.TrimRight(fmt.Sprintf(format, args...), "\n\t ")
	vlog.VI(2).Infof("%s: %s", loc, o)
	if s.t == nil {
		fmt.Fprint(os.Stderr, loc, o)
		return
	}
	s.t.Log(loc, o)
}

// ReportError calls Testing.Error to report any error currently stored
// in the Session.
func (s *Session) ReportError() {
	if s.err != nil && s.t != nil {
		s.t.Error(s.err)
	}
}

// error must always be called from a public function that is called
// directly by an external user, otherwise the file:line info will
// be incorrect.
func (s *Session) error(err error) error {
	_, file, line, _ := runtime.Caller(2)
	s.err = fmt.Errorf("%s:%d: %s", filepath.Base(file), line, err)
	s.ReportError()
	return s.err
}

type reader func(r *bufio.Reader) (string, error)

func readAll(r *bufio.Reader) (string, error) {
	all := ""
	for {
		l, err := r.ReadString('\n')
		all += l
		if err != nil {
			if err == io.EOF {
				return all, nil
			}
			return all, err
		}
	}
}

func readLine(r *bufio.Reader) (string, error) {
	return r.ReadString('\n')
}

func (s *Session) read(f reader) (string, error) {
	ch := make(chan string, 1)
	ech := make(chan error, 1)
	go func(fn reader, io *bufio.Reader) {
		str, err := fn(io)
		if err != nil {
			ech <- err
			return
		}
		ch <- str
	}(f, s.input)
	select {
	case err := <-ech:
		return "", err
	case m := <-ch:
		return m, nil
	case <-time.After(s.timeout):
		return "", Timeout
	}
}

// Expect asserts that the next line in the input matches the supplied string.
func (s *Session) Expect(expected string) {
	if s.Failed() {
		return
	}
	line, err := s.read(readLine)
	s.log("Expect: %v: %s", err, line)
	if err != nil {
		s.error(err)
		return
	}
	line = strings.TrimRight(line, "\n")

	if line != expected {
		s.error(fmt.Errorf("got %q, want %q", line, expected))
	}
	return
}

func (s *Session) expectRE(pattern string, n int) (string, [][]string, error) {
	if s.Failed() {
		return "", nil, s.err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", nil, err
	}
	line, err := s.read(readLine)
	if err != nil {
		return "", nil, err
	}
	line = strings.TrimRight(line, "\n")
	return line, re.FindAllStringSubmatch(line, n), err
}

// ExpectRE asserts that the next line in the input matches the pattern using
// regexp.MustCompile(pattern,n).FindAllStringSubmatch.
func (s *Session) ExpectRE(pattern string, n int) [][]string {
	if s.Failed() {
		return [][]string{}
	}
	l, m, err := s.expectRE(pattern, n)
	s.log("ExpectRE: %v: %s", err, l)
	if err != nil {
		s.error(err)
		return [][]string{}
	}
	if len(m) == 0 {
		s.error(fmt.Errorf("%q found no match in %q", pattern, l))
	}
	return m
}

// ExpectVar asserts that the next line in the input matches the pattern
// <name>=<value> and returns <value>.
func (s *Session) ExpectVar(name string) string {
	if s.Failed() {
		return ""
	}
	l, m, err := s.expectRE(name+"=(.*)", 1)
	s.log("ExpectVar: %v: %s", err, l)
	if err != nil {
		s.error(err)
		return ""
	}
	if len(m) != 1 || len(m[0]) != 2 {
		s.error(fmt.Errorf("failed to find value for %q in %q", name, l))
		return ""
	}
	return m[0][1]
}

// ReadLine reads the next line, if any, from the input stream. It will set
// the error state to io.EOF if it has read past the end of the stream.
// ReadLine has no effect if an error has already occurred.
func (s *Session) ReadLine() string {
	if s.Failed() {
		return ""
	}
	l, err := s.read(readLine)
	s.log("Readline: %v: %s", err, l)
	if err != nil {
		s.error(err)
	}
	return strings.TrimRight(l, "\n")
}

// ReadAll reads all remaining input on the stream. Unlike all of the other
// methods it does not strip newlines from the input.
// ReadAll has no effect if an error has already occurred.
func (s *Session) ReadAll() (string, error) {
	if s.Failed() {
		return "", s.err
	}
	return s.read(readAll)
}

// Finish reads all remaining input on the stream regardless of any
// prior errors and writes it to the supplied io.Writer parameter if non-nil.
// It returns both the data read and the prior error, if any, otherwise it
// returns any error that occurred reading the rest of the input.
func (s *Session) Finish(w io.Writer) (string, error) {
	a, err := s.read(readAll)
	if w != nil {
		fmt.Fprint(w, a)
	}
	if s.Failed() {
		return a, s.err
	}
	return a, err
}
