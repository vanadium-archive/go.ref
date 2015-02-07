// This package provides support for writing end-to-end style tests. The
// TestEnvironment type is the root of the API, you can use this type to set up
// your test environment and to perform operations within the environment. To
// create a new test environment, use the NewTestEnvironment method, e.g.
//
//   func TestFoo(t *testing.T) {
//     env := integration.NewTestEnvironment(t)
//     defer env.Cleanup()
//
//     ...
//   }
//
// The methods in this API typically do not return error in the case of
// failure. Instead, the current test will fail with an appropriate error
// message. This alleviates the need to handle errors in the test itself.
//
// End-to-end style tests may involve several communicating processes. These
// kinds of tests can be hard to debug using Go alone. The TestEnvironment
// interface provides a DebugShell() to assist in test debugging. This method
// will pause the current test and spawn a new shell that can be used to
// manually inspect and interact with the test environment.
package integration

import (
	"bytes"
	"container/list"
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

// TODO(cnicolaou): don't using testing.T's logging since it buffers it all
// up until the end of the job. Use vlog instead.
//
// TODO(cnicolaoi): instead of calling t.Fatalf etc, trap the error in the
// environment, wait for all processes, dump out their stderrs and then
// fail the test. This is the real benefit of having a wrapper like this.
// TODO(cnicolaou): it's not clear to me how we detect errors in crashed
// subprocesses, nor how we report such errors to the developer. This is
// one of the biggest problems with the shell based tests and I feel
// we need to do a better job to justify this effort.
//
// TODO(cnicolaou): need to enable running under the agent as per the old shell tests,
// via the shell_test::enable_agent "$@" mechanism.
//
// TODO(sjr): make this thread safe.
//
// TODO(sjr): we need I/O redirection and piping. This is one of the
// conveniences of the shell based tests that we've lost here. There's
// current no way to redirect I/O and it won't always be possible
// to change the command to take a command line arg. Similarly,
// we should provide support for piping output from one command to
// to another, or for creating process pipelines directly.
//
// TODO(sjr): need more testing of this core package, especially wrt to
// process cleanup, making sure debug output is captured correctly, etc.
//
// TODO(sjr): provide a utility function to retry an operation for a specific
// time before failing. Useful for synchronising across process startups etc.
//
// Test represents the currently running test. In a local end-to-end test
// environment obtained though New, this interface will be
// implemented by Go's standard testing.T.
//
// We are planning to implement a regression testing environment that does not
// depend on Go's testing framework. In this case, users of this interface will
// ideally not have to change their code to run on the new environment.
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

// T represents a test environment. Typically, an end-to-end
// test will begin with:
//   func TestFoo(t *testing.T) {
//     env := integration.New(t)
//     defer env.Cleanup()
//
//     ...
//   }
type T interface {
	Test

	// Cleanup cleans up the environment, deletes all its artifacts and
	// kills all subprocesses. It will kill subprocesses in LIFO order.
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

	// Returns a copy of this binary that, when Start is called, will use
	// the given environment variables.
	WithEnv(env []string) TestBinary
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
	Stdin() io.Writer
	Stdout() io.Reader

	// Session returns an expect.Session created for this invocation.
	// TODO(sjr): create a Session interface that expect.Session
	// implements and then embed that here so that we can call the
	// expect.Session methods directly on the invocation.
	Session() *expect.Session

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

// TODO(sjr): these log type names are not idiomatic for Go and are
// very hard to read. testEnvironment would be sufficient, testBinary etc.
type integrationTestEnvironment struct {
	// The testing framework.
	Test

	// The function to shutdown the context used to create the environment.
	shutdown veyron2.Shutdown

	// The shell to use to start commands.
	shell *modules.Shell

	// The environment's root security principal.
	principal security.Principal

	// Maps path to TestBinary.
	builtBinaries map[string]*integrationTestBinary

	tempFiles []*os.File
	tempDirs  []string

	invocations *list.List
}

type integrationTestBinary struct {
	// The environment to which this binary belongs.
	env *integrationTestEnvironment

	// The path to the binary.
	path string

	// Environment variables that will be used when creating invocations
	// via Start.
	envVars []string

	// The cleanup function to run when the binary exits.
	cleanupFunc func()
}

type integrationTestBinaryInvocation struct {
	// The environment to which this invocation belongs.
	env *integrationTestEnvironment

	// The handle to the process that was run when this invocation was started.
	handle modules.Handle

	// The element representing this invocation in the list of
	// invocations stored in the environment
	el *list.Element

	// The session that is created as a convenience for the user.
	session *expect.Session

	// The path of the binary used for this invocation.
	path string
}

func (i *integrationTestBinaryInvocation) Stdin() io.Writer {
	return i.handle.Stdin()
}

func (i *integrationTestBinaryInvocation) Stdout() io.Reader {
	return i.handle.Stdout()
}

func (i *integrationTestBinaryInvocation) Path() string {
	return i.path
}

func (i *integrationTestBinaryInvocation) Exists() bool {
	return syscall.Kill(i.handle.Pid(), 0) == nil
}

func (i *integrationTestBinaryInvocation) Kill(sig syscall.Signal) error {
	// TODO(sjr): consider using vexec to manage subprocesses.
	// TODO(sjr): if we use vexec, will want to tee stderr reliably to a file
	// as well as a stderr stream, maintain an 'enviroment' here.
	pid := i.handle.Pid()
	i.env.Logf("sending signal %v to PID %d", sig, pid)
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

func (i *integrationTestBinaryInvocation) Output() string {
	return readerToString(i.env, i.Stdout())
}

func (i *integrationTestBinaryInvocation) Wait(stdout, stderr io.Writer) error {
	i.env.removeInvocation(i.el)
	return i.handle.Shutdown(stdout, stderr)
}

func (i *integrationTestBinaryInvocation) WaitOrDie(stdout, stderr io.Writer) {
	if err := i.Wait(stdout, stderr); err != nil {
		i.env.Fatalf("FATAL: Wait() for pid %d failed: %v", i.handle.Pid(), err)
	}
}

func (i *integrationTestBinaryInvocation) Environment() T {
	return i.env
}

func (i *integrationTestBinaryInvocation) Session() *expect.Session {
	return i.session
}

func (b *integrationTestBinary) cleanup() {
	binaryDir := path.Dir(b.path)
	b.env.Logf("cleaning up %s", binaryDir)
	if err := os.RemoveAll(binaryDir); err != nil {
		b.env.Logf("WARNING: RemoveAll(%s) failed (%v)", binaryDir, err)
	}
}

func (b *integrationTestBinary) Path() string {
	return b.path
}

func (b *integrationTestBinary) Start(args ...string) Invocation {
	depth := testutil.DepthToExternalCaller()
	b.env.Logf(testutil.FormatLogLine(depth, "starting %s %s", b.Path(), strings.Join(args, " ")))
	handle, err := b.env.shell.StartExternalCommand(b.envVars, append([]string{b.Path()}, args...)...)
	if err != nil {
		b.env.Fatalf("StartExternalCommand(%v, %v) failed: %v", b.Path(), strings.Join(args, ", "), err)
	}
	b.env.Logf("started PID %d\n", handle.Pid())
	inv := &integrationTestBinaryInvocation{
		env:     b.env,
		handle:  handle,
		path:    b.path,
		session: expect.NewSession(b.env, handle.Stdout(), 5*time.Minute),
	}
	inv.el = b.env.appendInvocation(inv)
	return inv
}

func (b *integrationTestBinary) WithEnv(env []string) TestBinary {
	newBin := *b
	newBin.envVars = env
	return &newBin
}

func (e *integrationTestEnvironment) Principal() security.Principal {
	return e.principal
}

func (e *integrationTestEnvironment) Cleanup() {
	vlog.VI(1).Infof("V23Test.Cleanup")
	// Shut down all processes before attempting to delete any
	// files/directories to avoid potential 'file system busy' problems
	// on non-unix systems.
	for {
		// invocations may be modified by calls to Kill and Wait.
		// TODO(sjr): this will deadlock when we add locking. Revisit the
		// structure then.
		el := e.invocations.Front()
		if el == nil {
			break
		}
		inv := el.Value.(Invocation)
		vlog.VI(1).Infof("V23Test.Cleanup: Kill: %q", inv.Path())
		err := inv.Kill(syscall.SIGTERM)
		inv.Wait(os.Stdout, os.Stderr)
		vlog.VI(1).Infof("V23Test.Cleanup: Killed: %q: %v", inv.Path(), err)
	}

	vlog.Infof("V23Test.Cleanup: all invocations taken care of.")

	if err := e.shell.Cleanup(os.Stdout, os.Stderr); err != nil {
		e.Fatalf("WARNING: could not clean up shell (%v)", err)
	}

	for _, binary := range e.builtBinaries {
		binary.cleanupFunc()
	}

	for _, tempFile := range e.tempFiles {
		e.Logf("cleaning up %s", tempFile.Name())
		if err := tempFile.Close(); err != nil {
			e.Logf("WARNING: Close(%q) failed: %v", tempFile.Name(), err)
		}
		if err := os.RemoveAll(tempFile.Name()); err != nil {
			e.Logf("WARNING: RemoveAll(%q) failed: %v", tempFile.Name(), err)
		}
	}

	for _, tempDir := range e.tempDirs {
		e.Logf("cleaning up %s", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			e.Logf("WARNING: RemoveAll(%q) failed: %v", tempDir, err)
		}
	}

	e.shutdown()
}

func (e *integrationTestEnvironment) GetVar(key string) (string, bool) {
	return e.shell.GetVar(key)
}

func (e *integrationTestEnvironment) SetVar(key, value string) {
	e.shell.SetVar(key, value)
}

func (e *integrationTestEnvironment) ClearVar(key string) {
	e.shell.ClearVar(key)
}

func writeStringOrDie(t Test, f *os.File, s string) {
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
}

func (e *integrationTestEnvironment) DebugShell() {
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
		e.Logf("WARNING: Open(%v) failed, was asked to create a debug shell but cannot: %v", dev, err)
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

func (e *integrationTestEnvironment) BinaryFromPath(path string) TestBinary {
	return &integrationTestBinary{
		env:         e,
		envVars:     nil,
		path:        path,
		cleanupFunc: func() {},
	}
}

func (e *integrationTestEnvironment) BuildGoPkg(binary_path string) TestBinary {
	e.Logf("building %s...", binary_path)
	if cached_binary := e.builtBinaries[binary_path]; cached_binary != nil {
		e.Logf("using cached binary for %s at %s.", binary_path, cached_binary.Path())
		return cached_binary
	}
	built_path, cleanup, err := buildPkg(binary_path)
	if err != nil {
		e.Fatalf("buildPkg() failed: %v", err)
		return nil
	}
	output_path := path.Join(built_path, path.Base(binary_path))
	e.Logf("done building %s, written to %s.", binary_path, output_path)
	binary := &integrationTestBinary{
		env:         e,
		envVars:     nil,
		path:        output_path,
		cleanupFunc: cleanup,
	}
	e.builtBinaries[binary_path] = binary
	return binary
}

func (e *integrationTestEnvironment) TempFile() *os.File {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		e.Fatalf("TempFile() failed: %v", err)
	}
	e.Logf("created temporary file at %s", f.Name())
	e.tempFiles = append(e.tempFiles, f)
	return f
}

func (e *integrationTestEnvironment) TempDir() string {
	f, err := ioutil.TempDir("", "")
	if err != nil {
		e.Fatalf("TempDir() failed: %v", err)
	}
	e.Logf("created temporary directory at %s", f)
	e.tempDirs = append(e.tempDirs, f)
	return f
}

func (e *integrationTestEnvironment) appendInvocation(inv Invocation) *list.Element {
	return e.invocations.PushBack(inv)
}

func (e *integrationTestEnvironment) removeInvocation(el *list.Element) {
	e.invocations.Remove(el)
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

	t.Log("creating root principal")
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

	return &integrationTestEnvironment{
		Test:          t,
		principal:     principal,
		builtBinaries: make(map[string]*integrationTestBinary),
		shell:         shell,
		tempFiles:     []*os.File{},
		tempDirs:      []string{},
		shutdown:      shutdown,
		invocations:   list.New(),
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

// RunTest runs a single Vanadium integration test.
func RunTest(t Test, fn func(i T)) {
	if !testutil.RunIntegrationTests {
		t.Skip()
	}
	i := New(t)
	fn(i)
	i.Cleanup()
}

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
