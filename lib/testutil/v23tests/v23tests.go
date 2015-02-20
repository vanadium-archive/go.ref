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
// V23Test<x>(t *v23tests.T)
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
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/security/agent"
)

// TB is a mirror of testing.TB but without the private method
// preventing its implementation from outside of the go testing package.
type TB interface {
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
type T struct {
	// The embedded TB
	TB

	// The function to shutdown the context used to create the environment.
	shutdown veyron2.Shutdown

	// The shell to use to start commands.
	shell *modules.Shell

	// The environment's root security principal.
	principal security.Principal

	// Maps path to Binary.
	builtBinaries map[string]*Binary

	tempFiles    []*os.File
	tempDirs     []string
	cachedBinDir string
	dirStack     []string

	invocations []*Invocation
}

// Binary represents an executable program that will be executed during a
// test. A binary may be invoked multiple times by calling Start, each call
// will return a new Invocation.
//
// Binary instances are typically obtained from a T by calling BuildGoPkg
// (for Vanadium and other Go binaries) or BinaryFromPath (to start binaries
// that are already present on the system).
type Binary struct {
	// The environment to which this binary belongs.
	env *T

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

// Invocation represents a single invocation of a Binary.
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
type Invocation struct {
	*expect.Session

	// The environment to which this invocation belongs.
	env *T

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

// Stdin returns this invocations Stdin stream.
func (i *Invocation) Stdin() io.Writer {
	return i.handle.Stdin()
}

// CloseStdin closes the write-side of the pipe to the invocation's
// standard input.
func (i *Invocation) CloseStdin() {
	i.handle.CloseStdin()
}

// Stdout returns this invocations Stdout stream.
func (i *Invocation) Stdout() io.Reader {
	return i.handle.Stdout()
}

// Path returns the path to the binary that was used for this invocation.
func (i *Invocation) Path() string {
	return i.path
}

// Exists returns true if the invocation still exists.
func (i *Invocation) Exists() bool {
	return syscall.Kill(i.handle.Pid(), 0) == nil
}

// Sends the given signal to this invocation. It is up to the test
// author to decide whether failure to deliver the signal is fatal to
// the test.
func (i *Invocation) Kill(sig syscall.Signal) error {
	pid := i.handle.Pid()
	vlog.VI(1).Infof("sending signal %v to PID %d", sig, pid)
	return syscall.Kill(pid, sig)
}

// Caller returns a string of the form <filename>:<lineno> for the
// caller specified by skip, where skip is as per runtime.Caller.
func Caller(skip int) string {
	_, file, line, _ := runtime.Caller(skip + 1)
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

// Output reads the invocation's stdout until EOF and then returns what
// was read as a string.
func (i *Invocation) Output() string {
	buf := bytes.Buffer{}
	_, err := buf.ReadFrom(i.Stdout())
	if err != nil {
		i.env.Fatalf("%s: ReadFrom() failed: %v", Caller(1), err)
	}
	return buf.String()
}

// Wait waits for this invocation to finish. If either stdout or stderr
// is non-nil, any remaining unread output from those sources will be
// written to the corresponding writer. The returned error represents
// the exit status of the underlying command.
func (i *Invocation) Wait(stdout, stderr io.Writer) error {
	err := i.handle.Shutdown(stdout, stderr)
	i.hasShutdown = true
	i.shutdownErr = err
	return err
}

// Wait waits for this invocation to finish. If either stdout or stderr
// is non-nil, any remaining unread output from those sources will be
// written to the corresponding writer. If the underlying command
// exited with anything but success (exit status 0), this function will
// cause the current test to fail.
func (i *Invocation) WaitOrDie(stdout, stderr io.Writer) {
	if err := i.Wait(stdout, stderr); err != nil {
		i.env.Fatalf("%s: FATAL: Wait() for pid %d failed: %v", Caller(1), i.handle.Pid(), err)
	}
}

// Environment returns the instance of the test environment that this
// invocation was from.
func (i *Invocation) Environment() *T {
	return i.env
}

func (b *Binary) cleanup() {
	binaryDir := path.Dir(b.path)
	vlog.Infof("cleaning up %s", binaryDir)
	if err := os.RemoveAll(binaryDir); err != nil {
		vlog.Infof("WARNING: RemoveAll(%s) failed (%v)", binaryDir, err)
	}
}

// Path returns the path to the binary.
func (b *Binary) Path() string {
	return b.path
}

// Start starts the given binary with the given arguments.
func (b *Binary) Start(args ...string) *Invocation {
	return b.start(1, args...)
}

func (b *Binary) start(skip int, args ...string) *Invocation {
	vlog.Infof("%s: starting %s %s", Caller(skip+1), b.Path(), strings.Join(args, " "))
	handle, err := b.env.shell.StartExternalCommand(b.envVars, append([]string{b.Path()}, args...)...)
	if err != nil {
		// TODO(cnicolaou): calling Fatalf etc from a goroutine often leads
		// to deadlock. Need to make sure that we handle this here. Maybe
		// it's best to just return an error? Or provide a StartWithError
		// call for use from goroutines.
		b.env.Fatalf("%s: StartExternalCommand(%v, %v) failed: %v", Caller(skip+1), b.Path(), strings.Join(args, ", "), err)
	}
	vlog.Infof("started PID %d\n", handle.Pid())
	inv := &Invocation{
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
				vlog.Infof("%s: Copy failed: %v", Caller(skip+2), err)
			}
			inv.CloseStdin()
		}()
	}
	return inv
}

func (b *Binary) run(args ...string) string {
	inv := b.start(2, args...)
	var stdout, stderr bytes.Buffer
	err := inv.Wait(&stdout, &stderr)
	if err != nil {
		a := strings.Join(args, ", ")
		b.env.Fatalf("%s: Run(%s): failed: %v: \n%s\n", Caller(2), a, err, stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\n")
}

// Run runs the binary with the specified arguments to completion. On
// success it returns the contents of stdout, on failure it terminates the
// test with an error message containing the error and the contents of
// stderr.
func (b *Binary) Run(args ...string) string {
	return b.run(args...)
}

// Run constructs a Binary for path and invokes Run on it.
func (e *T) Run(path string, args ...string) string {
	return e.BinaryFromPath(path).run(args...)
}

// WaitFunc is the type of the functions to be used in conjunction
// with WaitFor and WaitForAsync. It should return a value or an error
// when it wants those functions to terminate, returning a nil value
// and nil error will result in it being called again after the specified
// delay time specified in the calls to WaitFor and WaitForAsync.
type WaitFunc func() (interface{}, error)

// WaitFor calls fn at least once with the specified delay value
// between iterations until the first of the following is encountered:
// 1. fn returns a non-nil value.
// 2. fn returns an error value
// 3. fn is executed at least once and the specified timeout is exceeded.
//
// WaitFor returns the non-nil value for the first case and calls e.Fatalf for
// the other two cases.
// WaitFor will always run fn at least once to completion and hence it will
// hang if that first iteration of fn hangs. If this behaviour is not
// appropriate, then WaitForAsync should be used.
func (e *T) WaitFor(fn WaitFunc, delay, timeout time.Duration) interface{} {
	deadline := time.Now().Add(timeout)
	for {
		val, err := fn()
		if val != nil {
			return val
		}
		if err != nil {
			e.Fatalf("%s: the WaitFunc returned an error: %v", Caller(1), err)
		}
		if time.Now().After(deadline) {
			e.Fatalf("%s: timedout after %s", Caller(1), timeout)
		}
		time.Sleep(delay)
	}
}

// WaitForAsync is like WaitFor except that it calls fn in a goroutine
// and can timeout during the execution fn.
func (e *T) WaitForAsync(fn WaitFunc, delay, timeout time.Duration) interface{} {
	resultCh := make(chan interface{})
	errCh := make(chan interface{})
	go func() {
		for {
			val, err := fn()
			if val != nil {
				resultCh <- val
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			time.Sleep(delay)
		}
	}()
	select {
	case err := <-errCh:
		e.Fatalf("%s: the WaitFunc returned error: %v", Caller(1), err)
	case result := <-resultCh:
		return result
	case <-time.After(timeout):
		e.Fatalf("%s: timedout after %s", Caller(1), timeout)
	}
	return nil
}

// Pushd pushes the current working directory to the stack of
// directories, returning it as its result, and changes the working
// directory to dir.
func (e *T) Pushd(dir string) string {
	cwd, err := os.Getwd()
	if err != nil {
		e.Fatalf("%s: Getwd failed: %s", Caller(1), err)
	}
	if err := os.Chdir(dir); err != nil {
		e.Fatalf("%s: Chdir(%s) failed: %s", Caller(1), dir, err)
	}
	e.dirStack = append(e.dirStack, cwd)
	return cwd
}

// Popd pops the most recent entry from the directory stack and changes
// the working directory to that directory. It returns the new working
// directory as its result.
func (e *T) Popd() string {
	if len(e.dirStack) == 0 {
		e.Fatalf("%s: directory stack empty", Caller(1))
	}
	dir := e.dirStack[len(e.dirStack)-1]
	e.dirStack = e.dirStack[:len(e.dirStack)-1]
	if err := os.Chdir(dir); err != nil {
		e.Fatalf("%s: Chdir(%s) failed: %s", Caller(1), dir, err)
	}
	return dir
}

// WithStdin returns a copy of this binary that, when Start is called,
// will read its input from the given reader. Once the reader returns
// EOF, the returned invocation's standard input will be closed (see
// Invocation.CloseStdin).
func (b *Binary) WithStdin(r io.Reader) *Binary {
	newBin := *b
	newBin.inputReader = r
	return &newBin
}

// Returns a copy of this binary that, when Start is called, will use
// the given environment variables. Each environment variable should be
// in "key=value" form. For example:
//
// bin.WithEnv("EXAMPLE_ENV=/tmp/something").Start(...)
func (b *Binary) WithEnv(env ...string) *Binary {
	newBin := *b
	newBin.envVars = env
	return &newBin
}

// Principal returns the security principal of this environment.
func (e *T) Principal() security.Principal {
	return e.principal
}

// Cleanup cleans up the environment, deletes all its artifacts and
// kills all subprocesses. It will kill subprocesses in LIFO order.
// Cleanup checks to see if the test has failed and logs information
// as to the state of the processes it was asked to invoke up to that
// point and optionally, if the --v23.tests.shell-on-fail flag is set
// then it will run a debug shell before cleaning up its state.
func (e *T) Cleanup() {
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
			if inv.shutdownErr != nil {
				m := fmt.Sprintf("%d: %s: shutdown status: %v", i, inv.path, inv.shutdownErr)
				e.Log(m)
				vlog.VI(1).Info(m)
				vlog.VI(2).Infof("%d: %s %v", i, inv.path, inv.args)
			}
		}
	}

	vlog.VI(1).Infof("V23Test.Cleanup")
	// Shut down all processes in LIFO order before attempting to delete any
	// files/directories to avoid potential 'file system busy' problems
	// on non-unix systems.
	for i := len(e.invocations); i > 0; i-- {
		inv := e.invocations[i-1]
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

// GetVar returns the variable associated with the specified key
// and an indication of whether it is defined or not.
func (e *T) GetVar(key string) (string, bool) {
	return e.shell.GetVar(key)
}

// SetVar sets the value to be associated with key.
func (e *T) SetVar(key, value string) {
	e.shell.SetVar(key, value)
}

// ClearVar removes the speficied variable from the Shell's environment
func (e *T) ClearVar(key string) {
	e.shell.ClearVar(key)
}

func writeStringOrDie(t *T, f *os.File, s string) {
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
}

// DebugShell drops the user into a debug shell with any environment
// variables specified in env... (in VAR=VAL format) available to it.
// If there is no controlling TTY, DebugShell will emit a warning message
// and take no futher action.
func (e *T) DebugShell(env ...string) {
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

	agentFile, err := e.shell.NewChildCredentials()
	if err != nil {
		vlog.Errorf("WARNING: failed to obtain credentials for the debug shell: %v", err)
	}

	file := os.NewFile(uintptr(fd), dev)
	attr := os.ProcAttr{
		Files: []*os.File{file, file, file},
		Dir:   cwd,
	}
	// Set up agent for Child.
	attr.Files = append(attr.Files, agentFile)
	attr.Env = append(attr.Env, fmt.Sprintf("%s=%d", agent.FdVarName, len(attr.Files)-1))

	// Set up environment for Child.
	for _, v := range e.shell.Env() {
		attr.Env = append(attr.Env, v)
	}

	for i, td := range e.tempDirs {
		attr.Env = append(attr.Env, fmt.Sprintf("V23_TMP_DIR%d=%s", i, td))
	}

	if len(e.cachedBinDir) > 0 {
		attr.Env = append(attr.Env, "V23_BIN_DIR="+e.cachedBinDir)
	}
	attr.Env = append(attr.Env, env...)

	// Start up a new shell.
	writeStringOrDie(e, file, ">> Starting a new interactive shell\n")
	writeStringOrDie(e, file, "Hit CTRL-D to resume the test\n")
	if len(e.builtBinaries) > 0 {
		writeStringOrDie(e, file, "Built binaries:\n")
		for _, value := range e.builtBinaries {
			writeStringOrDie(e, file, "\t"+value.Path()+"\n")
		}
	}
	if len(e.cachedBinDir) > 0 {
		writeStringOrDie(e, file, fmt.Sprintf("Binaries are cached in %q\n", e.cachedBinDir))
	} else {
		writeStringOrDie(e, file, "Caching of binaries was not enabled\n")
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

// BinaryFromPath returns a new Binary that, when started, will
// execute the executable or script at the given path.
//
// E.g. env.BinaryFromPath("/bin/bash").Start("-c", "echo hello world").Output() -> "hello world"
func (e *T) BinaryFromPath(path string) *Binary {
	return &Binary{
		env:         e,
		envVars:     nil,
		path:        path,
		cleanupFunc: func() {},
	}
}

// BuildGoPkg expects a Go package path that identifies a "main"
// package and returns a Binary representing the newly built
// binary.
func (e *T) BuildGoPkg(binary_path string) *Binary {
	then := time.Now()
	loc := Caller(1)
	cached, built_path, cleanup, err := buildPkg(e.cachedBinDir, binary_path)
	if err != nil {
		e.Fatalf("%s: buildPkg(%s) failed: %v", loc, binary_path, err)
		return nil
	}
	output_path := path.Join(built_path, path.Base(binary_path))

	if _, err := os.Stat(output_path); err != nil {
		e.Fatalf("%s: buildPkg(%s) failed to stat %q", loc, binary_path, output_path)
	}
	taken := time.Now().Sub(then)
	if cached {
		vlog.Infof("%s: using %s, from %s in %s.", loc, binary_path, output_path, taken)
	} else {
		vlog.Infof("%s: built %s, written to %s in %s.", loc, binary_path, output_path, taken)
	}

	binary := &Binary{
		env:         e,
		envVars:     nil,
		path:        output_path,
		cleanupFunc: cleanup,
	}
	e.builtBinaries[binary_path] = binary
	return binary
}

// NewTempFile creates a temporary file. Temporary files will be deleted
// by Cleanup.
func (e *T) NewTempFile() *os.File {
	loc := Caller(1)
	f, err := ioutil.TempFile("", "")
	if err != nil {
		e.Fatalf("%s: TempFile() failed: %v", loc, err)
	}
	vlog.Infof("%s: created temporary file at %s", loc, f.Name())
	e.tempFiles = append(e.tempFiles, f)
	return f
}

// NewTempDir creates a temporary directory. Temporary directories and
// their contents will be deleted by Cleanup.
func (e *T) NewTempDir() string {
	loc := Caller(1)
	f, err := ioutil.TempDir("", "")
	if err != nil {
		e.Fatalf("%s: TempDir() failed: %v", loc, err)
	}
	vlog.Infof("%s: created temporary directory at %s", loc, f)
	e.tempDirs = append(e.tempDirs, f)
	return f
}

func (e *T) appendInvocation(inv *Invocation) {
	e.invocations = append(e.invocations, inv)
}

// Creates a new local testing environment. A local testing environment has a
// a security principle available via Principal().
//
// You should clean up the returned environment using the env.Cleanup() method.
// A typical end-to-end test will begin like:
//
//   func TestFoo(t *testing.T) {
//     env := integration.NewT(t)
//     defer env.Cleanup()
//
//     ...
//   }
func New(t TB) *T {
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

	// The V23_BIN_DIR environment variable can be
	// used to identify a directory that multiple integration
	// tests can use to share binaries. Whoever sets this
	// environment variable is responsible for cleaning up the
	// directory it points to.
	cachedBinDir := os.Getenv("V23_BIN_DIR")
	return &T{
		TB:            t,
		principal:     principal,
		builtBinaries: make(map[string]*Binary),
		shell:         shell,
		tempFiles:     []*os.File{},
		tempDirs:      []string{},
		cachedBinDir:  cachedBinDir,
		shutdown:      shutdown,
	}
}

// BuildPkg returns a path to a directory that contains the built binary for
// the given packages and a function that should be invoked to clean up the
// build artifacts. Note that the clients of this function should not modify
// the contents of this directory directly and instead defer to the cleanup
// function.
func buildPkg(binDir, pkg string) (bool, string, func(), error) {
	cleanupFn := func() {}
	if len(binDir) == 0 {
		// If the aforementioned environment variable is not
		// set, the given packages are built in a temporary
		// directory, which the cleanup function removes.
		tmpDir, err := ioutil.TempDir("", "")
		if err != nil {
			return false, "", nil, fmt.Errorf("TempDir() failed: %v", err)
		}
		binDir, cleanupFn = tmpDir, func() { os.RemoveAll(tmpDir) }
	}
	binFile := filepath.Join(binDir, path.Base(pkg))
	cached := true
	if _, err := os.Stat(binFile); err != nil {
		if !os.IsNotExist(err) {
			return false, "", nil, err
		}
		cmd := exec.Command("v23", "go", "build", "-o", filepath.Join(binDir, path.Base(pkg)), pkg)
		if output, err := cmd.CombinedOutput(); err != nil {
			vlog.VI(1).Infof("\n%v:\n%v\n", strings.Join(cmd.Args, " "), string(output))
			return false, "", nil, err
		}
		cached = false
	}
	return cached, binDir, cleanupFn, nil
}

// RunTest runs a single Vanadium 'v23 style' integration test.
func RunTest(t *testing.T, fn func(i *T)) {
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
func RunRootMT(i *T, args ...string) (*Binary, *Invocation) {
	b := i.BuildGoPkg("v.io/core/veyron/services/mounttable/mounttabled")
	inv := b.start(1, args...)
	name := inv.ExpectVar("NAME")
	inv.Environment().SetVar("NAMESPACE_ROOT", name)
	vlog.Infof("Running root mount table: %q", name)
	return b, inv
}

// UseSharedBinDir ensures that a shared directory is used for binaries
// across multiple instances of the test environment. This is achieved
// by setting the V23_BIN_DIR environment variable if it is not already
// set in the test processes environment (as will typically be the case when
// these tests are run from the v23 tool). It is intended to be called
// from TestMain.
func UseSharedBinDir() func() {
	if v23BinDir := os.Getenv("V23_BIN_DIR"); len(v23BinDir) == 0 {
		v23BinDir, err := ioutil.TempDir("", "bin-")
		if err == nil {
			vlog.Infof("Setting V23_BIN_DIR to %q", v23BinDir)
			os.Setenv("V23_BIN_DIR", v23BinDir)
			return func() { os.RemoveAll(v23BinDir) }
		}
	} else {
		vlog.Infof("Using V23_BIN_DIR %q", v23BinDir)
	}
	return func() {}
}
