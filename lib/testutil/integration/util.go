package integration

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
	"veyron.io/veyron/veyron2/security"
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
}

type TestBinary interface {
	// Start starts the given binary with the given arguments.
	Start(args ...string) Invocation

	// Path returns the path to the binary.
	Path() string
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

	// Wait waits for this invocation to finish. If either stdout or stderr
	// is non-nil, any remaining unread output from those sources will be
	// written to the corresponding writer.
	Wait(stdout, stderr io.Writer)
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
}

type integrationTestBinary struct {
	// The environment to which this binary belongs.
	env *integrationTestEnvironment

	// The path to the binary.
	path string

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

func (i *integrationTestBinaryInvocation) Wait(stdout, stderr io.Writer) {
	if err := (*i.handle).Shutdown(stdout, stderr); err != nil {
		i.env.t.Fatalf("Shutdown() failed: %v", err)
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
	b.env.t.Logf("starting %s %s", b.Path(), strings.Join(args, " "))
	handle, err := b.env.shell.Start("exec", nil, append([]string{b.Path()}, args...)...)
	if err != nil {
		b.env.t.Fatalf("Start(%v, %v) failed: %v", b.Path(), strings.Join(args, ", "), err)
	}
	return &integrationTestBinaryInvocation{
		env:    b.env,
		handle: &handle,
	}
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
			e.t.Logf("WARNING: Close(%s) failed", tempFile, err)
		}
		if err := os.Remove(tempFile.Name()); err != nil {
			e.t.Logf("WARNING: Remove(%s) failed: %v", tempFile.Name(), err)
		}
	}

	if err := e.shell.Cleanup(os.Stdout, os.Stderr); err != nil {
		e.t.Fatalf("WARNING: could not clean up shell (%v)", err)
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
	fd, err := syscall.Open(dev, 0, 0)
	if err != nil {
		e.t.Logf("WARNING: Open(%v) failed, not going to create a debug shell: %v", dev, err)
		return
	}
	attr := os.ProcAttr{
		Files: []*os.File{os.NewFile(uintptr(fd), "/dev/tty"), os.Stdout, os.Stderr},
		Dir:   cwd,
	}

	// Start up a new shell.
	fmt.Printf(">> Starting a new interactive shell\n")
	fmt.Printf("Hit CTRL-D to resume the test\n")
	if len(e.builtBinaries) > 0 {
		fmt.Println("Built binaries:")
		for _, value := range e.builtBinaries {
			fmt.Println(value.Path())
		}
	}
	fmt.Println("Root mounttable endpoint:", e.RootMT())

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

	fmt.Printf("<< Exited shell: %s\n", state.String())
}

func (e *integrationTestEnvironment) BuildGoPkg(binary_path string) TestBinary {
	e.t.Logf("building %s...", binary_path)
	if cached_binary := e.builtBinaries[binary_path]; cached_binary != nil {
		e.t.Logf("using cached binary for %s at %s.", binary_path, cached_binary.Path())
		return cached_binary
	}
	built_path, cleanup, err := BuildPkgs([]string{binary_path})
	if err != nil {
		e.t.Fatalf("BuildPkgs() failed: %v", err)
		return nil
	}
	output_path := path.Join(built_path, path.Base(binary_path))
	e.t.Logf("done building %s, written to %s.", binary_path, output_path)
	binary := &integrationTestBinary{
		env:         e,
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

func NewTestEnvironment(t *testing.T) TestEnvironment {
	t.Log("creating root principal")
	principal := tsecurity.NewPrincipal("root")
	shell, err := modules.NewShell(principal)
	if err != nil {
		t.Fatalf("NewShell() failed: %v", err)
	}

	t.Log("starting root mounttable...")
	mtHandle, mtEndpoint, err := StartRootMT(shell)
	if err != nil {
		t.Fatalf("StartRootMT() failed: %v", err)
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
	}
}

// BuildPkgs returns a path to a directory that contains the built
// binaries for the given set of packages and a function that should
// be invoked to clean up the build artifacts. Note that the clients
// of this function should not modify the contents of this directory
// directly and instead defer to the cleanup function.
func BuildPkgs(pkgs []string) (string, func(), error) {
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
	for _, pkg := range pkgs {
		binFile := filepath.Join(binDir, path.Base(pkg))
		if _, err := os.Stat(binFile); err != nil {
			if !os.IsNotExist(err) {
				return "", nil, err
			}
			cmd := exec.Command("veyron", "go", "build", "-o", filepath.Join(binDir, path.Base(pkg)), pkg)
			if err := cmd.Run(); err != nil {
				return "", nil, err
			}
		}
	}
	return binDir, cleanupFn, nil
}

// StartRootMT uses the given shell to start a root mount table and
// returns a handle for the started command along with the object name
// of the mount table.
func StartRootMT(shell *modules.Shell) (modules.Handle, string, error) {
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
	s.ExpectVar("MT_ADDR")
	if err := s.Error(); err != nil {
		return nil, "", err
	}

	return handle, name, nil
}

// StartServer starts a veyron server using the given binary and
// arguments, waiting for the server to successfully mount itself in
// the mount table.
//
// TODO(jsimsa,sadovsky): Use an instance of modules.Shell to start
// and manage the server process to prevent leaking processes when
// its parent terminates unexpectedly.
func StartServer(bin string, args []string) (*os.Process, error) {
	args = append(args, "-logtostderr", "-vmodule=publisher=2")
	cmd := exec.Command(bin, args...)
	outPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	// TODO(jsimsa): Consider using the veyron exec library to
	// facilitate coordination and communication between the
	// parent and the child process.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%q failed: %v", strings.Join(cmd.Args, " "), err)
	}
	// Wait for the server to mount both its tcp and ws endpoint.
	ready := make(chan struct{}, 1)
	go func() {
		defer outPipe.Close()
		scanner := bufio.NewScanner(outPipe)
		mounts := 0
		for scanner.Scan() {
			line := scanner.Text()
			// TODO(cnicolaou): find a better way of synchronizing with
			// the child process, this is way too fragile.
			if strings.Index(line, "ipc pub: mount") != -1 {
				mounts++
				if mounts == 1 {
					close(ready)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Scan() failed: %v\n", err)
		}
	}()
	select {
	case <-ready:
		return cmd.Process, nil
	case <-time.After(time.Minute):
		cmd.Process.Kill()
		return nil, fmt.Errorf("timed out waiting for %q to mount itself", strings.Join(cmd.Args, " "))
	}
}
