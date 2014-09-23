package modules

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
)

type pipe struct {
	r, w *os.File
}
type functionHandle struct {
	mu                    sync.Mutex
	main                  Main
	stdin, stderr, stdout pipe
	err                   error
	sh                    *Shell
	wg                    sync.WaitGroup
}

func newFunctionHandle(main Main) command {
	return &functionHandle{main: main}
}

func (fh *functionHandle) Stdout() io.Reader {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	return fh.stdout.r
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
	fh.stdin.w.Close()
	fh.mu.Unlock()
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
	fh.wg.Add(1)

	go func() {
		fh.mu.Lock()
		stdin := fh.stdin.r
		stdout := fh.stdout.w
		stderr := fh.stderr.w
		main := fh.main
		fh.mu.Unlock()

		err := main(stdin, stdout, stderr, sh.mergeOSEnv(), args...)
		if err != nil {
			// Print the error to stdout to ensure that anyone reading
			// only stdout sees the error.
			fmt.Fprintf(stdout, "%s\n", err)
			fmt.Fprintf(stderr, "%s\n", err)
		}

		fh.mu.Lock()
		fh.stdin.r.Close()
		fh.stdout.w.Close()
		fh.stderr.w.Close()
		fh.err = err
		fh.mu.Unlock()
		fh.wg.Done()
	}()
	return fh, nil
}

func (fh *functionHandle) Shutdown(output io.Writer) error {
	fh.mu.Lock()
	fh.stdin.w.Close()
	stderr := fh.stderr.r
	fh.mu.Unlock()

	if output != nil {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			l := scanner.Text()
			fmt.Fprintf(output, "%s\n", l)
		}
	}

	fh.wg.Wait()
	fh.mu.Lock()
	fh.stdout.r.Close()
	fh.stderr.r.Close()
	err := fh.err
	fh.sh.forget(fh)
	fh.mu.Unlock()
	return err
}
