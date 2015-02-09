// Package v23tests provides support for writing end-to-end style integration
// tests. In particular, support is provided for building binaries, running
// processes, making assertions about their output/state and ensuring that
// no processes or files are left behind on exit. Since such tests are often
// difficult to debug facilities are provided to help do so.
//
// The preferred usage of this integration test framework is via the v23
// tool which generates supporting code. The primary reason for doing so is
// to cleanly separate integration tests, which can be very expensive to run,
// from normal unit tests which are intended to be fast and used constantly.
// However, it still beneficial to be able to always compile the integration
// test code with the normal test code, just not to run it. Similarly, it
// is beneficial to share as much of the existing go test infrastructure as
// possible, so the generated code uses a flag and a naming convention to
// separate the tests. Integration tests may be run in addition to unit tests
// by supplying the --v23.tests flag; the -run flag can be used
// to avoid running unit tests by specifying a prefix of TestV23 since
// the generate test functions always. Thus:
//
// v23 go test -v <pkgs> --v23.test  // runs both unit and integration tests
// v23 go test -v -run=TestV23 <pkgs> --v23.test // runs just integration tests
//
// The go generate mechanism is used to generate the test code, thus the
// comment:
//
// //go:generate v23 integration generate
//
// will generate the files v23_test.go and internal_v23_test.go for the
// package in which it occurs. Run v23 integration generate help for full
// details and options. In short, any function in an external
// (i.e. <pgk>_test) test package of the following form:
//
// V23Test<x>(t integration.T)
//
// will be invoked as integration test if the --v23.tests flag is used.
//
// The generated code makes use of the RunTest function, documented below.
//
// The test environment is implemented by an instance of the interface T.
// It is constructed with an instance of another interface Test, which is
// generally implemented by testing.T. Thus, the integration test environment
// directly as follows:
//
//   func TestFoo(t *testing.T) {
//     env := v23tests.New(t)
//     defer env.Cleanup()
//
//     ...
//   }
//
// The methods in this API typically do not return error in the case of
// failure. Instead, the current test will fail with an appropriate error
// message. This avoids the need to handle errors inline the test itself.
//
// The test environment manages all built packages, subprocesses and a
// set of environment variables that are passed to subprocesses.
//
// Debugging is supported as follows:
// 1. The DebugShell method creates an interative shell at that point in
//    the tests execution that has access to all of the running processes
//    and environment of those processes. The developer can interact with
//    those processes to determine the state of the test.
// 2. Calls to methods on Test (e.g. FailNow, Fatalf) that fail the test
//    cause the Cleanup method to print out the status of all invocations.
// 3. Similarly, if the --v23.tests.shell-on-error flag is set then the
//    cleanup method will invoke a DebugShell on a test failure allowing
//    the developer to inspect the state of the test.
// 4. The implementation of this package uses filenames that start with v23test
//    to allow for easy tracing with --vmodule=v23test*=2 for example.
//
package v23tests

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
)

// TODO(cnicolaou): need to enable running under the agent as per the old shell tests,
// via the shell_test::enable_agent "$@" mechanism.
//
//
// TODO(sjr,cnicolaou): caching of binaries is per test environment -
// it should be in a file system somewhere and should handle all tests run
// from a single invocation of v23.
//
// TODO(sjr): make this thread safe.
//
// TODO(sjr): document all of the methods.
//
// TODO(sjr): need more testing of this core package, especially wrt to
// process cleanup, making sure debug output is captured correctly, etc.
//
// TODO(sjr): provide a utility function to retry an operation for a specific
// time before failing. Useful for synchronising across process startups etc.
//
// Test represents the currently running test. In a local end-to-end test
// environment obtained though New, this interface will be
// typically be implemented by Go's standard testing.T.
type Test interface {
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fail()
	FailNow()
	Failed() bool
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Log(args ...interface{})
	Logf(format string, args ...interface{})
	Skip(args ...interface{})
	SkipNow()
	Skipf(format string, args ...interface{})
	Skipped() bool
}

// T represents an integration test environment.
type T interface {
	Test

	// Cleanup cleans up the environment, deletes all its artifacts and
	// kills all subprocesses. It will kill subprocesses in LIFO order.
	// Cleanup checks to see if the test has failed and logs information
	// as to the state of the processes it was asked to invoke up to that
	// point and optionally, if the --v23.tests.shell-on-fail flag is set
	// then it will run a debug shell before cleaning up its state.
	Cleanup()

	// BinaryFromPath returns a new TestBinary that, when started, will
	// execute the executable or script at the given path.
	//
	// E.g. env.BinaryFromPath("/bin/bash").Start("-c", "echo hello world").Output() -> "hello world"
	BinaryFromPath(path string) TestBinary

	// BuildGoPkg expects a Go package path that identifies a "main"
	// package and returns a TestBinary representing the newly built
	// binary.
	BuildGoPkg(path string) TestBinary

	// Principal returns the security principal of this environment.
	Principal() security.Principal

	// DebugShell drops the user into a debug shell. If there is no
	// controlling TTY, DebugShell will emit a warning message and take no
	// futher action.
	DebugShell()

	// TempFile creates a temporary file. Temporary files will be deleted
	// by Cleanup.
	TempFile() *os.File

	// TempDir creates a temporary directory. Temporary directories and
	// their contents will be deleted by Cleanup.
	TempDir() string

	// SetVar sets the value to be associated with key.
	SetVar(key, value string)

	// GetVar returns the variable associated with the specified key
	// and an indication of whether it is defined or not.
	GetVar(key string) (string, bool)

	// ClearVar removes the speficied variable from the Shell's environment
	ClearVar(key string)
}

type TestBinary interface {
	// Start starts the given binary with the given arguments.
	Start(args ...string) Invocation

	// Path returns the path to the binary.
	Path() string

	// WithEnv returns a copy of this binary that, when Start is called,
	// will use the given environment variables.
	WithEnv(env []string) TestBinary

	// WithStdin returns a copy of this binary that, when Start is called,
	// will read its input from the given reader. Once the reader returns
	// EOF, the returned invocation's standard input will be closed (see
	// Invocation.CloseStdin).
	WithStdin(r io.Reader) TestBinary
}

// Session mirrors veyron/lib/expect is used to allow us to embed all of
// expect.Session's methods in Invocation below.
type Session interface {
	Expect(expected string)
	Expectf(format string, args ...interface{})
	ExpectRE(pattern string, n int) [][]string
	ExpectVar(name string) string
	ExpectSetRE(expected ...string) [][]string
	ExpectSetEventuallyRE(expected ...string) [][]string
	ReadLine() string
	ExpectEOF() error
}

// Invocation represents a single invocation of a TestBinary.
//
// Any bytes written by the invocation to its standard error may be recovered
// using the Wait or WaitOrDie functions.
//
// For example:
//   bin := env.BinaryFromPath("/bin/bash")
//   inv := bin.Start("-c", "echo hello world 1>&2")
//   var stderr bytes.Buffer
//   inv.WaitOrDie(nil, &stderr)
//   // stderr.Bytes() now contains 'hello world\n'
type Invocation interface {
	Session

	Stdin() io.Writer

	// CloseStdin closes the write-side of the pipe to the invocation's
	// standard input.
	CloseStdin()

	Stdout() io.Reader

	// Output reads the invocation's stdout until EOF and then returns what
	// was read as a string.
	Output() string

	// Sends the given signal to this invocation. It is up to the test
	// author to decide whether failure to deliver the signal is fatal to
	// the test.
	Kill(syscall.Signal) error

	// Exists returns true if the invocation still exists.
	Exists() bool

	// Wait waits for this invocation to finish. If either stdout or stderr
	// is non-nil, any remaining unread output from those sources will be
	// written to the corresponding writer. The returned error represents
	// the exit status of the underlying command.
	Wait(stdout, stderr io.Writer) error

	// Wait waits for this invocation to finish. If either stdout or stderr
	// is non-nil, any remaining unread output from those sources will be
	// written to the corresponding writer. If the underlying command
	// exited with anything but success (exit status 0), this function will
	// cause the current test to fail.
	WaitOrDie(stdout, stderr io.Writer)

	// Environment returns the instance of the test environment that this
	// invocation was from.
	Environment() T

	// Path returns the path to the binary that was used for this invocation.
	Path() string
}

type testEnvironment struct {
	// The testing framework.
	Test

	// The function to shutdown the context used to create the environment.
	shutdown veyron2.Shutdown

	// The shell to use to start commands.
	shell *modules.Shell

	// The environment's root security principal.
	principal security.Principal

	// Maps path to TestBinary.
	builtBinaries map[string]*testBinary

	tempFiles []*os.File
	tempDirs  []string

	invocations []*testBinaryInvocation
}

type testBinary struct {
	// The environment to which this binary belongs.
	env *testEnvironment

	// The path to the binary.
	path string

	// Environment variables that will be used when creating invocations
	// via Start.
	envVars []string

	// The cleanup function to run when the binary exits.
	cleanupFunc func()

	// The reader who is supplying the bytes we're going to send to our stdin.
	inputReader io.Reader
}

type testBinaryInvocation struct {
	// The embedded Session
	Session

	// The environment to which this invocation belongs.
	env *testEnvironment

	// The handle to the process that was run when this invocation was started.
	handle modules.Handle

	// The element representing this invocation in the list of
	// invocations stored in the environment
	el *list.Element

	// The path of the binary used for this invocation.
	path string

	// The args the binary was started with
	args []string

	// True if the process has been shutdown
	hasShutdown bool

	// The error, if any, as determined when the invocation was
	// shutdown. It must be set to a default initial value of
	// errNotShutdown rather than nil to allow us to distinguish between
	// a successful shutdown or an error.
	shutdownErr error
}

var errNotShutdown = errors.New("has not been shutdown")

func (i *testBinaryInvocation) Stdin() io.Writer {
	return i.handle.Stdin()
}

func (i *testBinaryInvocation) CloseStdin() {
	i.handle.CloseStdin()
}

func (i *testBinaryInvocation) Stdout() io.Reader {
	return i.handle.Stdout()
}

func (i *testBinaryInvocation) Path() string {
	return i.path
}

func (i *testBinaryInvocation) Exists() bool {
	return syscall.Kill(i.handle.Pid(), 0) == nil
}

func (i *testBinaryInvocation) Kill(sig syscall.Signal) error {
	// TODO(sjr): consider using vexec to manage subprocesses.
	// TODO(sjr): if we use vexec, will want to tee stderr reliably to a file
	// as well as a stderr stream, maintain an 'enviroment' here.
	// We'll also want to maintain an environment, as per modules.Shell
	pid := i.handle.Pid()
	vlog.VI(1).Infof("sending signal %v to PID %d", sig, pid)
	return syscall.Kill(pid, sig)
}

func readerToString(t Test, r io.Reader) string {
	buf := bytes.Buffer{}
	_, err := buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("ReadFrom() failed: %v", err)
	}
	return buf.String()
}

func (i *testBinaryInvocation) Output() string {
	return readerToString(i.env, i.Stdout())
}

func (i *testBinaryInvocation) Wait(stdout, stderr io.Writer) error {
	err := i.handle.Shutdown(stdout, stderr)
	i.hasShutdown = true
	i.shutdownErr = err
	return err
}

func (i *testBinaryInvocation) WaitOrDie(stdout, stderr io.Writer) {
	if err := i.Wait(stdout, stderr); err != nil {
		i.env.Fatalf("FATAL: Wait() for pid %d failed: %v", i.handle.Pid(), err)
	}
}

func (i *testBinaryInvocation) Environment() T {
	return i.env
}

func (b *testBinary) cleanup() {
	binaryDir := path.Dir(b.path)
	vlog.Infof("cleaning up %s", binaryDir)
	if err := os.RemoveAll(binaryDir); err != nil {
		vlog.Infof("WARNING: RemoveAll(%s) failed (%v)", binaryDir, err)
	}
}

func (b *testBinary) Path() string {
	return b.path
}

func (b *testBinary) Start(args ...string) Invocation {
	depth := testutil.DepthToExternalCaller()
	vlog.Infof(testutil.FormatLogLine(depth, "starting %s %s", b.Path(), strings.Join(args, " ")))
	handle, err := b.env.shell.StartExternalCommand(b.envVars, append([]string{b.Path()}, args...)...)
	if err != nil {
		b.env.Fatalf("StartExternalCommand(%v, %v) failed: %v", b.Path(), strings.Join(args, ", "), err)
	}
	vlog.Infof("started PID %d\n", handle.Pid())
	inv := &testBinaryInvocation{
		env:         b.env,
		handle:      handle,
		path:        b.path,
		args:        args,
		shutdownErr: errNotShutdown,
		Session:     expect.NewSession(b.env, handle.Stdout(), 5*time.Minute),
	}
	b.env.appendInvocation(inv)
	if b.inputReader != nil {
		// This goroutine makes a best-effort attempt to copy bytes
		// from b.inputReader to inv.Stdin() using io.Copy. When Copy
		// returns (successfully or not), inv.CloseStdin() is called.
		// This is always safe, even if inv has been shutdown.
		//
		// This goroutine's lifespan will be limited to that of the
		// environment to which 'inv' is attached. This is because the
		// environment will take care to kill all remaining invocations
		// upon Cleanup, which will in turn cause Copy to fail and
		// therefore this goroutine will exit.
		go func() {
			if _, err := io.Copy(inv.Stdin(), b.inputReader); err != nil {
				vlog.Infof("Copy failed: %v", err)
			}
			inv.CloseStdin()
		}()
	}
	return inv
}

func (b *testBinary) WithStdin(r io.Reader) TestBinary {
	newBin := *b
	newBin.inputReader = r
	return &newBin
}

func (b *testBinary) WithEnv(env []string) TestBinary {
	newBin := *b
	newBin.envVars = env
	return &newBin
}

func (e *testEnvironment) Principal() security.Principal {
	return e.principal
}

func (e *testEnvironment) Cleanup() {
	if e.Failed() {
		if testutil.IntegrationTestsDebugShellOnError {
			e.DebugShell()
		}
		// Print out a summary of the invocations and their status.
		for i, inv := range e.invocations {
			if inv.hasShutdown && inv.Exists() {
				m := fmt.Sprintf("%d: %s has been shutdown but still exists: %v", i, inv.path, inv.shutdownErr)
				e.Log(m)
				vlog.VI(1).Info(m)
				vlog.VI(2).Infof("%d: %s %v", i, inv.path, inv.args)
				continue
			}
			m := fmt.Sprintf("%d: %s: shutdown status: %v", i, inv.path, inv.shutdownErr)
			e.Log(m)
			vlog.VI(1).Info(m)
			vlog.VI(2).Infof("%d: %s %v", i, inv.path, inv.args)
		}
	}

	vlog.VI(1).Infof("V23Test.Cleanup")
	// Shut down all processes before attempting to delete any
	// files/directories to avoid potential 'file system busy' problems
	// on non-unix systems.
	for _, inv := range e.invocations {
		if inv.hasShutdown {
			vlog.VI(1).Infof("V23Test.Cleanup: %q has been shutdown", inv.Path())
			continue
		}
		vlog.VI(1).Infof("V23Test.Cleanup: Kill: %q", inv.Path())
		err := inv.Kill(syscall.SIGTERM)
		inv.Wait(os.Stdout, os.Stderr)
		vlog.VI(1).Infof("V23Test.Cleanup: Killed: %q: %v", inv.Path(), err)
	}
	vlog.VI(1).Infof("V23Test.Cleanup: all invocations taken care of.")

	if err := e.shell.Cleanup(os.Stdout, os.Stderr); err != nil {
		e.Fatalf("WARNING: could not clean up shell (%v)", err)
	}

	vlog.VI(1).Infof("V23Test.Cleanup: cleaning up binaries & files")

	for _, binary := range e.builtBinaries {
		binary.cleanupFunc()
	}

	for _, tempFile := range e.tempFiles {
		vlog.VI(1).Infof("V23Test.Cleanup: cleaning up %s", tempFile.Name())
		if err := tempFile.Close(); err != nil {
			vlog.Errorf("WARNING: Close(%q) failed: %v", tempFile.Name(), err)
		}
		if err := os.RemoveAll(tempFile.Name()); err != nil {
			vlog.Errorf("WARNING: RemoveAll(%q) failed: %v", tempFile.Name(), err)
		}
	}

	for _, tempDir := range e.tempDirs {
		vlog.VI(1).Infof("V23Test.Cleanup: cleaning up %s", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			vlog.Errorf("WARNING: RemoveAll(%q) failed: %v", tempDir, err)
		}
	}

	// shutdown the runtime
	e.shutdown()
}

func (e *testEnvironment) GetVar(key string) (string, bool) {
	return e.shell.GetVar(key)
}

func (e *testEnvironment) SetVar(key, value string) {
	e.shell.SetVar(key, value)
}

func (e *testEnvironment) ClearVar(key string) {
	e.shell.ClearVar(key)
}

func writeStringOrDie(t Test, f *os.File, s string) {
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
}

func (e *testEnvironment) DebugShell() {
	// Get the current working directory.
	cwd, err := os.Getwd()
	if err != nil {
		e.Fatalf("Getwd() failed: %v", err)
	}

	// Transfer stdin, stdout, and stderr to the new process
	// and also set target directory for the shell to start in.
	dev := "/dev/tty"
	fd, err := syscall.Open(dev, syscall.O_RDWR, 0)
	if err != nil {
		vlog.Errorf("WARNING: Open(%v) failed, was asked to create a debug shell but cannot: %v", dev, err)
		return
	}
	file := os.NewFile(uintptr(fd), dev)
	attr := os.ProcAttr{
		Files: []*os.File{file, file, file},
		Dir:   cwd,
	}

	// Set up environment for Child.
	if ns, ok := e.GetVar("NAMESPACE_ROOT"); ok {
		attr.Env = append(attr.Env, "NAMESPACE_ROOT="+ns)
	}
	// TODO(sjr): talk to Ankur about how to do this properly/safely
	// using either the agent, or a file descriptor inherited by the shell
	// and its children. This is preferable since it avoids compiling and
	// running the agent which can be an overhead in tests.
	dir, _ := tsecurity.ForkCredentials(e.principal, "debugShell")
	attr.Env = append(attr.Env, "VEYRON_CRED="+dir)

	// Start up a new shell.
	writeStringOrDie(e, file, ">> Starting a new interactive shell\n")
	writeStringOrDie(e, file, "Hit CTRL-D to resume the test\n")
	if len(e.builtBinaries) > 0 {
		writeStringOrDie(e, file, "Built binaries:\n")
		for _, value := range e.builtBinaries {
			writeStringOrDie(e, file, "\t"+value.Path()+"\n")
		}
	}

	shellPath := "/bin/sh"
	if shellPathFromEnv := os.Getenv("SHELL"); shellPathFromEnv != "" {
		shellPath = shellPathFromEnv
	}
	proc, err := os.StartProcess(shellPath, []string{}, &attr)
	if err != nil {
		e.Fatalf("StartProcess(%q) failed: %v", shellPath, err)
	}

	// Wait until user exits the shell
	state, err := proc.Wait()
	if err != nil {
		e.Fatalf("Wait(%v) failed: %v", shellPath, err)
	}

	writeStringOrDie(e, file, fmt.Sprintf("<< Exited shell: %s\n", state.String()))
}

func (e *testEnvironment) BinaryFromPath(path string) TestBinary {
	return &testBinary{
		env:         e,
		envVars:     nil,
		path:        path,
		cleanupFunc: func() {},
	}
}

func (e *testEnvironment) BuildGoPkg(binary_path string) TestBinary {
	vlog.Infof("building %s...", binary_path)
	if cached_binary := e.builtBinaries[binary_path]; cached_binary != nil {
		vlog.Infof("using cached binary for %s at %s.", binary_path, cached_binary.Path())
		return cached_binary
	}
	built_path, cleanup, err := buildPkg(binary_path)
	if err != nil {
		e.Fatalf("buildPkg() failed: %v", err)
		return nil
	}
	output_path := path.Join(built_path, path.Base(binary_path))
	vlog.Infof("done building %s, written to %s.", binary_path, output_path)
	binary := &testBinary{
		env:         e,
		envVars:     nil,
		path:        output_path,
		cleanupFunc: cleanup,
	}
	e.builtBinaries[binary_path] = binary
	return binary
}

func (e *testEnvironment) TempFile() *os.File {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		e.Fatalf("TempFile() failed: %v", err)
	}
	vlog.Infof("created temporary file at %s", f.Name())
	e.tempFiles = append(e.tempFiles, f)
	return f
}

func (e *testEnvironment) TempDir() string {
	f, err := ioutil.TempDir("", "")
	if err != nil {
		e.Fatalf("TempDir() failed: %v", err)
	}
	vlog.Infof("created temporary directory at %s", f)
	e.tempDirs = append(e.tempDirs, f)
	return f
}

func (e *testEnvironment) appendInvocation(inv *testBinaryInvocation) {
	e.invocations = append(e.invocations, inv)
}

// Creates a new local testing environment. A local testing environment has a
// a security principle available via Principal().
//
// You should clean up the returned environment using the env.Cleanup() method.
// A typical end-to-end test will begin like:
//
//   func TestFoo(t *testing.T) {
//     env := integration.NewTestEnvironment(t)
//     defer env.Cleanup()
//
//     ...
//   }
func New(t Test) T {
	ctx, shutdown := veyron2.Init()

	vlog.Infof("creating root principal")
	principal := tsecurity.NewPrincipal("root")
	ctx, err := veyron2.SetPrincipal(ctx, principal)
	if err != nil {
		t.Fatalf("failed to set principal: %v", err)
	}

	shell, err := modules.NewShell(ctx, principal)
	if err != nil {
		t.Fatalf("NewShell() failed: %v", err)
	}
	shell.SetStartTimeout(1 * time.Minute)
	shell.SetWaitTimeout(5 * time.Minute)

	return &testEnvironment{
		Test:          t,
		principal:     principal,
		builtBinaries: make(map[string]*testBinary),
		shell:         shell,
		tempFiles:     []*os.File{},
		tempDirs:      []string{},
		shutdown:      shutdown,
	}
}

// BuildPkg returns a path to a directory that contains the built binary for
// the given packages and a function that should be invoked to clean up the
// build artifacts. Note that the clients of this function should not modify
// the contents of this directory directly and instead defer to the cleanup
// function.
func buildPkg(pkg string) (string, func(), error) {
	// The VEYRON_INTEGRATION_BIN_DIR environment variable can be
	// used to identify a directory that multiple integration
	// tests can use to share binaries. Whoever sets this
	// environment variable is responsible for cleaning up the
	// directory it points to.
	binDir, cleanupFn := os.Getenv("VEYRON_INTEGRATION_BIN_DIR"), func() {}
	if binDir == "" {
		// If the aforementioned environment variable is not
		// set, the given packages are built in a temporary
		// directory, which the cleanup function removes.
		tmpDir, err := ioutil.TempDir("", "")
		if err != nil {
			return "", nil, fmt.Errorf("TempDir() failed: %v", err)
		}
		binDir, cleanupFn = tmpDir, func() { os.RemoveAll(tmpDir) }
	}
	binFile := filepath.Join(binDir, path.Base(pkg))
	if _, err := os.Stat(binFile); err != nil {
		if !os.IsNotExist(err) {
			return "", nil, err
		}
		cmd := exec.Command("v23", "go", "build", "-o", filepath.Join(binDir, path.Base(pkg)), pkg)
		if err := cmd.Run(); err != nil {
			return "", nil, err
		}
	}
	return binDir, cleanupFn, nil
}

// RunTest runs a single Vanadium 'v23 style' integration test.
func RunTest(t Test, fn func(i T)) {
	if !testutil.IntegrationTestsEnabled {
		t.Skip()
	}
	i := New(t)
	// defer the Cleanup method so that it will be called even if
	// t.Fatalf/FailNow etc are called and can print out useful information.
	defer i.Cleanup()
	fn(i)
}

// RunRootMT builds and runs a root mount table instance. It populates
// the NAMESPACE_ROOT variable in the test environment so that all subsequent
// invocations will access this root mount table.
func RunRootMT(t T, args ...string) (TestBinary, Invocation) {
	b := t.BuildGoPkg("v.io/core/veyron/services/mounttable/mounttabled")
	i := b.Start(args...)
	s := expect.NewSession(t, i.Stdout(), time.Minute)
	name := s.ExpectVar("NAME")
	i.Environment().SetVar("NAMESPACE_ROOT", name)
	return b, i
}

// TODO(sjr): provided convenience wrapper for dealing with credentials if
// necessary.
