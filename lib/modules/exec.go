package modules

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/vlog"

	vexec "veyron.io/veyron/veyron/lib/exec"
)

// execHandle implements both the command and Handle interfaces.
type execHandle struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	entryPoint string
	handle     *vexec.ParentHandle
	sh         *Shell
	stderr     *os.File
	stdout     io.ReadCloser
	stdin      io.WriteCloser
}

func testFlags() []string {
	var fl []string
	// pass logging flags to any subprocesses
	for fname, fval := range vlog.Log.ExplicitlySetFlags() {
		fl = append(fl, "--"+fname+"="+fval)
	}
	timeout := flag.Lookup("test.timeout")
	if timeout == nil {
		// not a go test binary
		return fl
	}
	// must be a go test binary
	fl = append(fl, "-test.run=TestHelperProcess")
	val := timeout.Value.(flag.Getter).Get().(time.Duration)
	if val.String() != timeout.DefValue {
		// use supplied command value for subprocesses
		fl = append(fl, "--test.timeout="+timeout.Value.String())
	} else {
		// translate default value into 1m for subproccesses
		fl = append(fl, "--test.timeout=1m")
	}
	return fl
}

// IsTestHelperProces returns true if it is called in via
// -run=TestHelperProcess which normally only ever happens for subprocesses
// run from tests.
func IsTestHelperProcess() bool {
	runFlag := flag.Lookup("test.run")
	if runFlag == nil {
		return false
	}
	return runFlag.Value.String() == "TestHelperProcess"
}

func newExecHandle(entryPoint string) command {
	return &execHandle{entryPoint: entryPoint}
}

func (eh *execHandle) Stdout() io.Reader {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	return eh.stdout
}

func (eh *execHandle) Stderr() io.Reader {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	return eh.stderr
}

func (eh *execHandle) Stdin() io.Writer {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	return eh.stdin
}

func (eh *execHandle) CloseStdin() {
	eh.mu.Lock()
	eh.stdin.Close()
	eh.mu.Unlock()
}

// mergeOSEnv returns a slice contained the merged set of environment
// variables from the OS environment and those in this Shell, preferring
// values in the Shell environment over those found in the OS environment.
func (sh *Shell) mergeOSEnvSlice() []string {
	merged := sh.mergeOSEnv()
	env := []string{}
	for k, v := range merged {
		env = append(env, k+"="+v)
	}
	return env
}

func osEnvironMap() map[string]string {
	m := make(map[string]string)
	for _, osv := range os.Environ() {
		if len(osv) == 0 {
			continue
		}
		parts := strings.SplitN(osv, "=", 2)
		key := parts[0]
		if len(parts) == 2 {
			m[key] = parts[1]
		} else {
			m[key] = ""
		}
	}
	return m
}
func (sh *Shell) mergeOSEnv() map[string]string {
	merged := osEnvironMap()
	sh.mu.Lock()
	for k, v := range sh.env {
		merged[k] = v
	}
	sh.mu.Unlock()
	return merged
}

func (eh *execHandle) start(sh *Shell, args ...string) (Handle, error) {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	eh.sh = sh
	newargs := append(testFlags(), args...)
	cmd := exec.Command(os.Args[0], newargs...)
	cmd.Env = append(sh.mergeOSEnvSlice(), eh.entryPoint)
	fname := strings.TrimPrefix(eh.entryPoint, ShellEntryPoint+"=")
	stderr, err := newLogfile(strings.TrimLeft(fname, "-\n\t "))
	if err != nil {
		return nil, err
	}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	handle := vexec.NewParentHandle(cmd)
	eh.stdout = stdout
	eh.stderr = stderr
	eh.stdin = stdin
	eh.handle = handle
	eh.cmd = cmd
	if err := handle.Start(); err != nil {
		return nil, err
	}
	err = handle.WaitForReady(10 * time.Second)
	return eh, err
}

func (eh *execHandle) Pid() int {
	return eh.cmd.Process.Pid
}

func (eh *execHandle) Shutdown(stdout, stderr io.Writer) error {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	eh.stdin.Close()
	logFile := eh.stderr.Name()
	defer eh.sh.forget(eh)

	defer func() {
		os.Remove(logFile)
	}()

	if stdout == nil && stderr == nil {
		return eh.cmd.Wait()
	}
	// Read from stdin before waiting for the child process to ensure
	// that we get to read all of its output.
	readTo(eh.stdout, stdout)

	procErr := eh.cmd.Wait()

	// Stderr is buffered to a file, so we can safely read it after we
	// wait for the process.
	eh.stderr.Close()
	stderrFile, err := os.Open(logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open %q: %s", logFile, err)
		return procErr
	}
	readTo(stderrFile, stderr)
	stderrFile.Close()
	return procErr
}

const ShellEntryPoint = "VEYRON_SHELL_HELPER_PROCESS_ENTRY_POINT"

func RegisterChild(name string, main Main) {
	child.Lock()
	defer child.Unlock()
	child.mains[name] = main
}

// DispatchInTest will execute the requested subproccess command from within
// a unit test run as a subprocess.
func DispatchInTest() {
	if !IsTestHelperProcess() {
		return
	}
	if err := child.dispatch(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Dispatch will execute the requested subprocess command from a within a
// a subprocess that is not a unit test.
func Dispatch() error {
	if IsTestHelperProcess() {
		return fmt.Errorf("use DispatchInTest in unittests")
	}
	return child.dispatch()
}

func (child *childRegistrar) hasCommand(name string) bool {
	child.Lock()
	_, present := child.mains[name]
	child.Unlock()
	return present
}

func (child *childRegistrar) dispatch() error {
	command := os.Getenv(ShellEntryPoint)
	if len(command) == 0 {
		return fmt.Errorf("Failed to find entrypoint %q", ShellEntryPoint)
	}
	child.Lock()
	m := child.mains[command]
	child.Unlock()
	if m == nil {
		return fmt.Errorf("Shell command %q not registered", command)
	}
	go func(pid int) {
		for {
			_, err := os.FindProcess(pid)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Looks like our parent exited: %v", err)
				os.Exit(1)
			}
			time.Sleep(time.Second)
		}
	}(os.Getppid())
	ch, err := vexec.GetChildHandle()
	if err == nil {
		// Only signal that the child is ready if we successfully get
		// a child handle. We most likely failed to get a child handle
		// because the subprocess was run directly from the command line.
		ch.SetReady()
	}
	return m(os.Stdin, os.Stdout, os.Stderr, osEnvironMap(), flag.Args()...)
}

// WaitForEof returns when a read on its io.Reader parameter returns io.EOF
func WaitForEOF(stdin io.Reader) {
	buf := [1024]byte{}
	for {
		if _, err := stdin.Read(buf[:]); err == io.EOF {
			return
		}
	}
}
