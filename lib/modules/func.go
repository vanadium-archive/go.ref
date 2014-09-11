package modules

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

type pipe struct {
	r, w *os.File
}
type functionHandle struct {
	mu                    sync.Mutex
	main                  Main
	stdin, stderr, stdout pipe
	bufferedStdout        *bufio.Reader
}

func newFunctionHandle(main Main) command {
	return &functionHandle{main: main}
}

func (fh *functionHandle) Stdout() *bufio.Reader {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.bufferedStdout
}

func (fh *functionHandle) Stderr() io.Reader {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.stderr.r
}

func (fh *functionHandle) Stdin() io.WriteCloser {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.stdin.w
}

func (fh *functionHandle) start(sh *Shell, args ...string) (Handle, error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	for _, p := range []*pipe{&fh.stdin, &fh.stdout, &fh.stderr} {
		var err error
		if p.r, p.w, err = os.Pipe(); err != nil {
			return nil, err
		}
	}
	fh.bufferedStdout = bufio.NewReader(fh.stdout.r)
	go func() {
		err := fh.main(fh.stdin.r, fh.stdout.w, fh.stderr.w, sh.mergeOSEnv(), args...)
		if err != nil {
			fmt.Fprintf(fh.stderr.w, "%s\n", err)
		}
		// See the comment below in Shutdown.
		syscall.Close(int(fh.stdin.r.Fd()))
		syscall.Close(int(fh.stdout.w.Fd()))
		syscall.Close(int(fh.stderr.w.Fd()))
	}()
	return fh, nil
}

func (fh *functionHandle) Shutdown(output io.Writer) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	scanner := bufio.NewScanner(fh.stderr.r)
	for scanner.Scan() {
		fmt.Fprintf(output, "%s\n", scanner.Text())
	}
	// We close these files using the Close system call since there
	// may be an oustanding read on them that would otherwise trigger
	// a test failure with go test -race
	syscall.Close(int(fh.stdin.w.Fd()))
	syscall.Close(int(fh.stdout.r.Fd()))
	syscall.Close(int(fh.stderr.r.Fd()))
}
