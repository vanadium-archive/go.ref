package modules

import (
	"fmt"
	"io"
	"os"
	"sync"

	"veyron.io/veyron/veyron2/vlog"
)

type pipe struct {
	r, w *os.File
}
type functionHandle struct {
	mu            sync.Mutex
	name          string
	main          Main
	stdin, stdout pipe
	stderr        *os.File
	err           error
	sh            *Shell
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
	return fh.stdin.w
}

func (fh *functionHandle) CloseStdin() {
	fh.mu.Lock()
	fh.stdin.w.Close()
	fh.mu.Unlock()
}

func (fh *functionHandle) envelope(sh *Shell, env []string, args ...string) ([]string, []string) {
	return args, env
}

func (fh *functionHandle) start(sh *Shell, env []string, args ...string) (Handle, error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.sh = sh
	for _, p := range []*pipe{&fh.stdin, &fh.stdout} {
		var err error
		if p.r, p.w, err = os.Pipe(); err != nil {
			return nil, err
		}
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
		stdout := fh.stdout.w
		stderr := fh.stderr
		main := fh.main
		fh.mu.Unlock()

		cenv := envSliceToMap(env)
		vlog.VI(1).Infof("Start: %q args: %v", fh.name, args)
		vlog.VI(2).Infof("Start: %q env: %v", fh.name, cenv)
		err := main(stdin, stdout, stderr, cenv, args...)
		if err != nil {
			fmt.Fprintf(stderr, "%s\n", err)
		}

		fh.mu.Lock()
		fh.stdin.r.Close()
		fh.stdout.w.Close()
		fh.err = err
		fh.mu.Unlock()
		fh.wg.Done()
	}(env)
	return fh, nil
}

func (eh *functionHandle) Pid() int {
	return os.Getpid()
}

func (fh *functionHandle) Shutdown(stdout_w, stderr_w io.Writer) error {
	fh.mu.Lock()
	vlog.VI(1).Infof("Shutdown: %q", fh.name)
	fh.stdin.w.Close()
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
	stderr.Close()
	if stderr_w != nil {
		if stderr, err := os.Open(stderr.Name()); err == nil {
			io.Copy(stderr_w, stderr)
			stderr.Close()
		} else {
			fmt.Fprintf(os.Stderr, "failed to open %q: %s\n", stderr.Name(), err)
		}
	}

	fh.mu.Lock()
	fh.stdout.r.Close()
	fh.sh.Forget(fh)
	fh.mu.Unlock()
	return funcErr
}
