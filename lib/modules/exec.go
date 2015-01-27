package modules

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	vexec "v.io/core/veyron/lib/exec"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/vlog"
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
	procErrCh  chan error
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
		// translate default value into 3m for subproccesses.  The
		// default of 10m is too long to wait in order to find out that
		// our subprocess is wedged.
		fl = append(fl, "--test.timeout=3m")
	}
	return fl
}

// IsTestHelperProcess returns true if it is called in via
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
	return &execHandle{name: name, entryPoint: shellEntryPoint + "=" + name, procErrCh: make(chan error, 1)}
}

func newExecHandleForExternalCommand(name string) command {
	return &execHandle{name: name, procErrCh: make(chan error, 1)}
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

func (eh *execHandle) start(sh *Shell, agentfd *os.File, env []string, args ...string) (Handle, error) {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	eh.sh = sh
	cmdPath := args[0]
	newargs, newenv := args, env

	// If an entry point is specified, use the envelope execution environment.
	if eh.entryPoint != "" {
		cmdPath = os.Args[0]
		newargs, newenv = eh.envelope(sh, env, args[1:]...)
	}

	cmd := exec.Command(cmdPath, newargs[1:]...)
	cmd.Env = newenv
	stderr, err := newLogfile("stderr", eh.name)
	if err != nil {
		return nil, err
	}
	cmd.Stderr = stderr
	// We use a custom queue-based Writer implementation for stdout to
	// decouple the consumers of eh.stdout from the file where the child
	// sends its output.  This avoids data races between closing the file
	// and reading from it (since cmd.Wait will wait for the all readers to
	// be done before closing it).  It also enables Shutdown to drain stdout
	// while respecting the timeout.
	stdout := newRW()
	cmd.Stdout = stdout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	config := vexec.NewConfig()
	serialized, err := sh.config.Serialize()
	if err != nil {
		return nil, err
	}
	config.MergeFrom(serialized)
	if agentfd != nil {
		childfd := len(cmd.ExtraFiles) + vexec.FileOffset
		config.Set(mgmt.SecurityAgentFDConfigKey, strconv.Itoa(childfd))
		cmd.ExtraFiles = append(cmd.ExtraFiles, agentfd)
		defer agentfd.Close()
	}

	handle := vexec.NewParentHandle(cmd, vexec.ConfigOpt{Config: config})
	eh.stdout = stdout
	eh.stderr = stderr
	eh.stdin = stdin
	eh.handle = handle
	eh.cmd = cmd
	vlog.VI(1).Infof("Start: %q stderr: %s", eh.name, stderr.Name())
	vlog.VI(1).Infof("Start: %q args: %v", eh.name, cmd.Args)
	vlog.VI(2).Infof("Start: %q env: %v", eh.name, cmd.Env)
	if err := handle.Start(); err != nil {
		return nil, err
	}
	vlog.VI(1).Infof("Started: %q, pid %d", eh.name, cmd.Process.Pid)
	go func() {
		eh.procErrCh <- eh.handle.Wait(0)
		// It's now safe to close eh.stdout, since Wait only returns
		// once all writes from the pipe to the stdout Writer have
		// completed.  Closing eh.stdout lets consumers of stdout wrap
		// up (they'll receive EOF).
		eh.stdout.Close()
	}()

	return eh, nil
}

func (eh *execHandle) Pid() int {
	return eh.cmd.Process.Pid
}

func (eh *execHandle) Shutdown(stdout, stderr io.Writer) error {
	eh.mu.Lock()
	defer eh.mu.Unlock()
	vlog.VI(1).Infof("Shutdown: %q", eh.name)
	defer vlog.VI(1).Infof("Shutdown: %q [DONE]", eh.name)
	eh.stdin.Close()
	defer eh.sh.Forget(eh)

	waitStdout := make(chan struct{})
	if stdout != nil {
		// Drain stdout.
		go func() {
			io.Copy(stdout, eh.stdout)
			close(waitStdout)
		}()
	} else {
		close(waitStdout)
	}

	var procErr error
	select {
	case procErr = <-eh.procErrCh:
		// The child has exited already.
	case <-time.After(eh.sh.waitTimeout):
		// Time out waiting for child to exit.
		procErr = vexec.ErrTimeout
		// Force close stdout to unblock any readers of stdout
		// (including the drain loop started above).
		eh.stdout.Close()
	}
	<-waitStdout

	// Transcribe stderr.
	outputFromFile(eh.stderr, stderr)
	os.Remove(eh.stderr.Name())

	return procErr
}

func (eh *execHandle) WaitForReady(timeout time.Duration) error {
	return eh.handle.WaitForReady(timeout)
}
