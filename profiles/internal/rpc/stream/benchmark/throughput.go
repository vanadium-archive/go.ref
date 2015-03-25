// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"crypto/rand"
	"io"
	"sync"
	"testing"
)

const (
	// Number of bytes to read/write
	throughputBlockSize = 50 << 10 // 50 KB
)

type throughputTester struct {
	b       *testing.B
	writers []io.WriteCloser
	readers []io.ReadCloser

	data    []byte
	pending sync.WaitGroup
}

func (t *throughputTester) Run() {
	t.pending.Add(len(t.writers) + len(t.readers))
	iters := t.b.N / len(t.writers)
	t.data = make([]byte, throughputBlockSize)
	if n, err := rand.Read(t.data); n != len(t.data) || err != nil {
		t.b.Fatalf("Failed to fill write buffer with data: (%d, %v)", n, err)
	}
	t.b.ResetTimer()
	for _, w := range t.writers {
		go t.writeLoop(w, iters)
	}
	for _, r := range t.readers {
		go t.readLoop(r)
	}
	t.pending.Wait()
}

func (t *throughputTester) writeLoop(w io.WriteCloser, N int) {
	defer t.pending.Done()
	defer w.Close()
	size := len(t.data)
	t.b.SetBytes(int64(size))
	for i := 0; i < N; i++ {
		if n, err := w.Write(t.data); err != nil || n != size {
			t.b.Fatalf("Write error: %v", err)
			return
		}
	}
}

func (t *throughputTester) readLoop(r io.ReadCloser) {
	defer t.pending.Done()
	defer r.Close()
	var buf [throughputBlockSize]byte
	total := 0
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			if err != io.EOF {
				t.b.Errorf("Read returned (%d, %v)", n, err)
			}
			break
		}
		total += n
	}
}
