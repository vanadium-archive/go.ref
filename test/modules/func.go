package modules

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"v.io/x/lib/vlog"
	"v.io/x/ref/test/expect"
)

type pipe struct {
	// We use the *os.File when we create pipe's that we need
	// to close etc, but we use the io.Reader when we have
	// a stdin stream specified via StartOpts. In that case we
	// don't have a *os.File that we can close.
	rf, wf *os.File
	r      io.Reader
}
type functionHandle struct {
	*expect.Session
	mu            sync.Mutex
	name          string
	main          Main
	stdin, stdout pipe
	stderr        *os.File
	err           error
	sh            *Shell
	opts          *StartOpts
	wg            sync.WaitGroup
}

func newFunctionHandle(name string, main Main) command {
	return &functionHandle{name: name, main: main}
}

func (fh *functionHandle) Stdout() io.Reader {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.stdout.r
}

func (fh *functionHandle) Stderr() io.Reader {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return os.NewFile(fh.stderr.Fd(), "stderr")
}

func (fh *functionHandle) Stdin() io.Writer {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.stdin.wf
}

func (fh *functionHandle) CloseStdin() {
	fh.mu.Lock()
	if fh.stdin.wf != nil {
		fh.stdin.wf.Close()
	}
	fh.mu.Unlock()
}

func (fh *functionHandle) envelope(sh *Shell, env []string, args ...string) ([]string, []string) {
	return args, env
}

func (fh *functionHandle) start(sh *Shell, agent *os.File, opts *StartOpts, env []string, args ...string) (Handle, error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.opts = opts
	// In process commands need their own reference to a principal.
	if agent != nil {
		agent.Close()
	}
	fh.sh = sh

	var pipes []*pipe
	if fh.opts.Stdin != nil {
		pipes = []*pipe{&fh.stdout}
		fh.stdin.r = fh.opts.Stdin
	} else {
		pipes = []*pipe{&fh.stdin, &fh.stdout}
	}

	for _, p := range pipes {
		var err error
		if p.rf, p.wf, err = os.Pipe(); err != nil {
			return nil, err
		}
		p.r, _ = p.rf, p.wf
	}

	stderr, err := newLogfile("stderr", args[0])
	if err != nil {
		return nil, err
	}
	fh.stderr = stderr
	fh.wg.Add(1)

	go func(env []string) {
		fh.mu.Lock()
		stdin := fh.stdin.r
		stdout := io.Writer(fh.stdout.wf)
		stderr := fh.stderr
		main := fh.main
		fh.mu.Unlock()

		cenv := envSliceToMap(env)
		vlog.VI(1).Infof("Start: %q args: %v", fh.name, args)
		vlog.VI(2).Infof("Start: %q env: %v", fh.name, cenv)
		err := main(stdin, stdout, stderr, cenv, args[1:]...)
		if err != nil {
			fmt.Fprintf(stderr, "%s\n", err)
		}

		fh.mu.Lock()
		if fh.stdin.rf != nil {
			fh.stdin.rf.Close()
		}
		fh.stdout.wf.Close()
		fh.err = err
		fh.mu.Unlock()
		fh.wg.Done()
	}(env)
	fh.Session = expect.NewSession(opts.ExpectTesting, fh.stdout.r, opts.ExpectTimeout)
	fh.Session.SetVerbosity(fh.sh.sessionVerbosity)
	return fh, nil
}

func (eh *functionHandle) Pid() int {
	return os.Getpid()
}

func (fh *functionHandle) Shutdown(stdout_w, stderr_w io.Writer) error {
	fh.mu.Lock()
	vlog.VI(1).Infof("Shutdown: %q", fh.name)
	if fh.stdin.wf != nil {
		fh.stdin.wf.Close()
	}
	stdout := fh.stdout.r
	stderr := fh.stderr
	fh.mu.Unlock()

	// Read stdout until EOF to ensure that we read all of it.
	if stdout_w != nil {
		io.Copy(stdout_w, stdout)
	}
	fh.wg.Wait()

	fh.mu.Lock()
	funcErr := fh.err
	fh.mu.Unlock()

	// Safe to close stderr now.
	stderrName := stderr.Name()
	stderr.Close()
	defer os.Remove(stderrName)
	if stderr_w != nil {
		if stderr, err := os.Open(stderrName); err == nil {
			io.Copy(stderr_w, stderr)
			stderr.Close()
		} else {
			fmt.Fprintf(os.Stderr, "failed to open %q: %s\n", stderrName, err)
		}
	}

	fh.mu.Lock()
	if fh.stdout.rf != nil {
		fh.stdout.rf.Close()
	}
	fh.sh.Forget(fh)
	fh.mu.Unlock()
	return funcErr
}

func (fh *functionHandle) WaitForReady(time.Duration) error {
	return nil
}
