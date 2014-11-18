package modules

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	vexec "veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron2/vlog"
)

// execHandle implements both the command and Handle interfaces.
type execHandle struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	name       string
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

func newExecHandle(name string) command {
	return &execHandle{name: name, entryPoint: shellEntryPoint + "=" + name}
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

func (eh *execHandle) envelope(sh *Shell, env []string, args ...string) ([]string, []string) {
	newargs := []string{os.Args[0]}
	newargs = append(newargs, testFlags()...)
	newargs = append(newargs, args...)
	// Be careful to remove any existing shellEntryPoint env vars. This
	// can happen when subprocesses run other subprocesses etc.
	cleaned := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, shellEntryPoint+"=") {
			continue
		}
		cleaned = append(cleaned, e)
	}
	return newargs, append(cleaned, eh.entryPoint)
}

func (eh *execHandle) start(sh *Shell, env []string, args ...string) (Handle, error) {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	eh.sh = sh
	newargs, newenv := eh.envelope(sh, env, args[1:]...)
	cmd := exec.Command(os.Args[0], newargs[1:]...)
	cmd.Env = newenv
	stderr, err := newLogfile(strings.TrimLeft(eh.name, "-\n\t "))
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

	handle := vexec.NewParentHandle(cmd, vexec.ConfigOpt{Config: sh.config})
	eh.stdout = stdout
	eh.stderr = stderr
	eh.stdin = stdin
	eh.handle = handle
	eh.cmd = cmd
	vlog.VI(1).Infof("Start: %q args: %v", eh.name, cmd.Args)
	vlog.VI(2).Infof("Start: %q env: %v", eh.name, cmd.Env)
	if err := handle.Start(); err != nil {
		return nil, err
	}
	vlog.VI(1).Infof("Started: %q, pid %d", eh.name, cmd.Process.Pid)
	err = handle.WaitForReady(sh.startTimeout)
	return eh, err
}

func (eh *execHandle) Pid() int {
	return eh.cmd.Process.Pid
}

func (eh *execHandle) Shutdown(stdout, stderr io.Writer) error {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	vlog.VI(1).Infof("Shutdown: %q", eh.name)
	eh.stdin.Close()
	logFile := eh.stderr.Name()
	defer eh.sh.Forget(eh)

	defer func() {
		os.Remove(logFile)
	}()

	// TODO(cnicolaou): make this configurable
	timeout := 10 * time.Second
	if stdout == nil && stderr == nil {
		return eh.handle.Wait(timeout)
	}

	if stdout != nil {
		// Read from stdin before waiting for the child process to ensure
		// that we get to read all of its output.
		readTo(eh.stdout, stdout)
	}

	procErr := eh.handle.Wait(timeout)

	// Stderr is buffered to a file, so we can safely read it after we
	// wait for the process.
	eh.stderr.Close()
	if stderr != nil {
		stderrFile, err := os.Open(logFile)
		if err != nil {
			vlog.VI(1).Infof("failed to open %q: %s\n", logFile, err)
			return procErr
		}
		readTo(stderrFile, stderr)
		stderrFile.Close()
	}
	return procErr
}
