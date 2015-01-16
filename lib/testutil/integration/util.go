package integration

import (
	"bytes"
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

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/modules/core"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
)

// TestEnvironment represents a test environment. You should obtain
// an instance with NewTestEnvironment. Typically, an end-to-end
// test will begin with:
//   func TestFoo(t *testing.T) {
//     env := integration.NewTestEnvironment(t)
//     defer env.Cleanup()
//
//     ...
//   }
type TestEnvironment interface {
	// Cleanup cleans up the environment and deletes all its artifacts.
	Cleanup()

	// BuildGoPkg expects a Go package path that identifies a "main"
	// package and returns a TestBinary representing the newly built
	// binary.
	BuildGoPkg(path string) TestBinary

	// RootMT returns the endpoint to the root mounttable for this test
	// environment.
	RootMT() string

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

type Invocation interface {
	Stdin() io.Writer
	Stdout() io.Reader
	Stderr() io.Reader

	// Output reads the invocation's stdout until EOF and then returns what
	// was read as a string.
	Output() string

	// ErrorOutput reads the invocation's stderr until EOF and then returns
	// what was read as a string.
	ErrorOutput() string

	// Sends the given signal to this invocation. It is up to the test
	// author to decide whether failure to deliver the signal is fatal to
	// the test.
	Kill(syscall.Signal) error

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
}

type integrationTestEnvironment struct {
	// The testing framework.
	t *testing.T

	// The shell to use to start commands.
	shell *modules.Shell

	// The environment's root security principal.
	principal security.Principal

	// Maps path to TestBinary.
	builtBinaries map[string]*integrationTestBinary

	mtHandle   *modules.Handle
	mtEndpoint string

	tempFiles []*os.File
	tempDirs  []string
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
	handle *modules.Handle
}

func (i *integrationTestBinaryInvocation) Stdin() io.Writer {
	return (*i.handle).Stdin()
}

func (i *integrationTestBinaryInvocation) Stdout() io.Reader {
	return (*i.handle).Stdout()
}

func (i *integrationTestBinaryInvocation) Kill(sig syscall.Signal) error {
	pid := (*i.handle).Pid()
	(*i.handle).Shutdown(nil, nil)
	i.env.t.Logf("sending signal %v to PID %d", sig, pid)
	return syscall.Kill(pid, sig)
}

func readerToString(t *testing.T, r io.Reader) string {
	buf := bytes.Buffer{}
	_, err := buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("ReadFrom() failed: %v", err)
	}
	return buf.String()
}

func (i *integrationTestBinaryInvocation) Output() string {
	return readerToString(i.env.t, i.Stdout())
}

func (i *integrationTestBinaryInvocation) Stderr() io.Reader {
	return (*i.handle).Stderr()
}

func (i *integrationTestBinaryInvocation) ErrorOutput() string {
	return readerToString(i.env.t, i.Stderr())
}

func (i *integrationTestBinaryInvocation) Wait(stdout, stderr io.Writer) error {
	return (*i.handle).Shutdown(stdout, stderr)
}

func (i *integrationTestBinaryInvocation) WaitOrDie(stdout, stderr io.Writer) {
	if err := i.Wait(stdout, stderr); err != nil {
		i.env.t.Fatalf("FATAL: Wait() for pid %d failed: %v", (*i.handle).Pid(), err)
	}
}

func (b *integrationTestBinary) cleanup() {
	binaryDir := path.Dir(b.path)
	b.env.t.Logf("cleaning up %s", binaryDir)
	if err := os.RemoveAll(binaryDir); err != nil {
		b.env.t.Logf("WARNING: RemoveAll(%s) failed (%v)", binaryDir, err)
	}
}

func (b *integrationTestBinary) Path() string {
	return b.path
}

func (b *integrationTestBinary) Start(args ...string) Invocation {
	locationString := ""
	if _, file, line, ok := runtime.Caller(1); ok {
		locationString = fmt.Sprintf("(requested at %s:%d) ", filepath.Base(file), line)
	}
	b.env.t.Logf("%sstarting %s %s", locationString, b.Path(), strings.Join(args, " "))
	handle, err := b.env.shell.Start("exec", b.envVars, append([]string{b.Path()}, args...)...)
	if err != nil {
		b.env.t.Fatalf("Start(%v, %v) failed: %v", b.Path(), strings.Join(args, ", "), err)
	}
	b.env.t.Logf("started PID %d\n", handle.Pid())
	return &integrationTestBinaryInvocation{
		env:    b.env,
		handle: &handle,
	}
}

func (b *integrationTestBinary) WithEnv(env []string) TestBinary {
	newBin := *b
	newBin.envVars = env
	return &newBin
}

func (e *integrationTestEnvironment) RootMT() string {
	return e.mtEndpoint
}

func (e *integrationTestEnvironment) Principal() security.Principal {
	return e.principal
}

func (e *integrationTestEnvironment) Cleanup() {
	for _, binary := range e.builtBinaries {
		binary.cleanupFunc()
	}

	for _, tempFile := range e.tempFiles {
		e.t.Logf("cleaning up %s", tempFile.Name())
		if err := tempFile.Close(); err != nil {
			e.t.Logf("WARNING: Close(%q) failed: %v", tempFile.Name(), err)
		}
		if err := os.RemoveAll(tempFile.Name()); err != nil {
			e.t.Logf("WARNING: RemoveAll(%q) failed: %v", tempFile.Name(), err)
		}
	}

	for _, tempDir := range e.tempDirs {
		e.t.Logf("cleaning up %s", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			e.t.Logf("WARNING: RemoveAll(%q) failed: %v", tempDir, err)
		}
	}

	if err := e.shell.Cleanup(os.Stdout, os.Stderr); err != nil {
		e.t.Fatalf("WARNING: could not clean up shell (%v)", err)
	}
}

func writeStringOrDie(t *testing.T, f *os.File, s string) {
	if _, err := f.WriteString(s); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
}

func (e *integrationTestEnvironment) DebugShell() {
	// Get the current working directory.
	cwd, err := os.Getwd()
	if err != nil {
		e.t.Fatalf("Getwd() failed: %v", err)
	}

	// Transfer stdin, stdout, and stderr to the new process
	// and also set target directory for the shell to start in.
	dev := "/dev/tty"
	fd, err := syscall.Open(dev, syscall.O_RDWR, 0)
	if err != nil {
		e.t.Logf("WARNING: Open(%v) failed, was asked to create a debug shell but cannot: %v", dev, err)
		return
	}
	file := os.NewFile(uintptr(fd), dev)
	attr := os.ProcAttr{
		Files: []*os.File{file, file, file},
		Dir:   cwd,
	}

	// Start up a new shell.
	writeStringOrDie(e.t, file, ">> Starting a new interactive shell\n")
	writeStringOrDie(e.t, file, "Hit CTRL-D to resume the test\n")
	if len(e.builtBinaries) > 0 {
		writeStringOrDie(e.t, file, "Built binaries:\n")
		for _, value := range e.builtBinaries {
			writeStringOrDie(e.t, file, "\t"+value.Path()+"\n")
		}
	}
	writeStringOrDie(e.t, file, fmt.Sprintf("Root mounttable endpoint: %s\n", e.RootMT()))

	shellPath := "/bin/sh"
	proc, err := os.StartProcess(shellPath, []string{}, &attr)
	if err != nil {
		e.t.Fatalf("StartProcess(%v) failed: %v", shellPath, err)
	}

	// Wait until user exits the shell
	state, err := proc.Wait()
	if err != nil {
		e.t.Fatalf("Wait(%v) failed: %v", shellPath, err)
	}

	writeStringOrDie(e.t, file, fmt.Sprintf("<< Exited shell: %s\n", state.String()))
}

func (e *integrationTestEnvironment) BuildGoPkg(binary_path string) TestBinary {
	e.t.Logf("building %s...", binary_path)
	if cached_binary := e.builtBinaries[binary_path]; cached_binary != nil {
		e.t.Logf("using cached binary for %s at %s.", binary_path, cached_binary.Path())
		return cached_binary
	}
	built_path, cleanup, err := buildPkg(binary_path)
	if err != nil {
		e.t.Fatalf("buildPkg() failed: %v", err)
		return nil
	}
	output_path := path.Join(built_path, path.Base(binary_path))
	e.t.Logf("done building %s, written to %s.", binary_path, output_path)
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
		e.t.Fatalf("TempFile() failed: %v", err)
	}
	e.t.Logf("created temporary file at %s", f.Name())
	e.tempFiles = append(e.tempFiles, f)
	return f
}

func (e *integrationTestEnvironment) TempDir() string {
	f, err := ioutil.TempDir("", "")
	if err != nil {
		e.t.Fatalf("TempDir() failed: %v", err)
	}
	e.t.Logf("created temporary directory at %s", f)
	e.tempDirs = append(e.tempDirs, f)
	return f
}

func NewTestEnvironment(t *testing.T) TestEnvironment {
	t.Log("creating root principal")
	principal := tsecurity.NewPrincipal("root")
	runtime, err := rt.New(options.RuntimePrincipal{principal})
	if err != nil {
		t.Fatalf("rt.New() failed: %v", err)
	}
	shell, err := modules.NewShell(runtime.NewContext(), principal)
	if err != nil {
		t.Fatalf("NewShell() failed: %v", err)
	}

	t.Log("starting root mounttable...")
	mtHandle, mtEndpoint, err := startRootMT(shell)
	if err != nil {
		t.Fatalf("startRootMT() failed: %v", err)
	}
	t.Logf("mounttable available at %s", mtEndpoint)

	return &integrationTestEnvironment{
		t:             t,
		principal:     principal,
		builtBinaries: make(map[string]*integrationTestBinary),
		shell:         shell,
		mtHandle:      &mtHandle,
		mtEndpoint:    mtEndpoint,
		tempFiles:     []*os.File{},
		tempDirs:      []string{},
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

// startRootMT uses the given shell to start a root mount table and
// returns a handle for the started command along with the object name
// of the mount table.
func startRootMT(shell *modules.Shell) (modules.Handle, string, error) {
	handle, err := shell.Start(core.RootMTCommand, nil, "--", "--veyron.tcp.address=127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	s := expect.NewSession(nil, handle.Stdout(), 10*time.Second)
	s.ExpectVar("PID")
	if err := s.Error(); err != nil {
		return nil, "", err
	}
	name := s.ExpectVar("MT_NAME")
	if err := s.Error(); err != nil {
		return nil, "", err
	}

	return handle, name, nil
}
