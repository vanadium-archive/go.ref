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
	err                   error
	sh                    *Shell
	wg                    sync.WaitGroup
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

func (fh *functionHandle) Stdin() io.Writer {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.stdin.w
}

func (fh *functionHandle) CloseStdin() {
	fh.mu.Lock()
	fd := fh.stdin.w.Fd()
	fh.mu.Unlock()
	syscall.Close(int(fd))
}

func (fh *functionHandle) start(sh *Shell, args ...string) (Handle, error) {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	fh.sh = sh
	for _, p := range []*pipe{&fh.stdin, &fh.stdout, &fh.stderr} {
		var err error
		if p.r, p.w, err = os.Pipe(); err != nil {
			return nil, err
		}
	}
	fh.bufferedStdout = bufio.NewReader(fh.stdout.r)
	fh.wg.Add(1)

	go func() {
		err := fh.main(fh.stdin.r, fh.stdout.w, fh.stderr.w, sh.mergeOSEnv(), args...)
		if err != nil {
			fmt.Fprintf(fh.stderr.w, "%s\n", err)
		}
		fh.mu.Lock()
		// We close these files using the Close system call since there
		// may be an oustanding read on them that would otherwise trigger
		// a test failure with go test -race
		syscall.Close(int(fh.stdin.w.Fd()))
		syscall.Close(int(fh.stdout.r.Fd()))
		syscall.Close(int(fh.stderr.r.Fd()))
		fh.err = err
		fh.mu.Unlock()
		fh.wg.Done()
	}()
	return fh, nil
}

func (fh *functionHandle) Shutdown(output io.Writer) error {
	fh.mu.Lock()
	syscall.Close(int(fh.stdin.w.Fd()))
	if output != nil {
		scanner := bufio.NewScanner(fh.stderr.r)
		for scanner.Scan() {
			fmt.Fprintf(output, "%s\n", scanner.Text())
		}
	}
	fh.mu.Unlock()

	fh.wg.Wait()

	fh.mu.Lock()
	err := fh.err
	fh.sh.forget(fh)
	fh.mu.Unlock()
	return err
}
