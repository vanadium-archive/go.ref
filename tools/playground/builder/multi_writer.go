// MultiWriter creates a writer that duplicates its writes to all the
// provided writers, similar to the Unix tee(1) command.
//
// Similar to http://golang.org/src/pkg/io/multi.go.

package main

import (
	"io"
	"sync"
)

// Initialize using newMultiWriter.
type multiWriter struct {
	writers []io.Writer
	mu      sync.Mutex
	wrote   bool
}

var _ io.Writer = (*multiWriter)(nil)

func newMultiWriter() *multiWriter {
	return &multiWriter{writers: []io.Writer{}}
}

// Returns self for convenience.
func (t *multiWriter) Add(w io.Writer) *multiWriter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.wrote {
		panic("Tried to add writer after data has been written.")
	}
	t.writers = append(t.writers, w)
	return t
}

func (t *multiWriter) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	t.wrote = true
	t.mu.Unlock()
	for _, w := range t.writers {
		n, err = w.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}
