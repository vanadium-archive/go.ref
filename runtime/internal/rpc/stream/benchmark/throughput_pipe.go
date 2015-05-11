// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"io"
	"os"
	"testing"
)

// benchmarkPipe runs a benchmark to test the throughput when nPipes each are
// reading and writing.
func benchmarkPipe(b *testing.B, nPipes int) {
	readers := make([]io.ReadCloser, nPipes)
	writers := make([]io.WriteCloser, nPipes)
	var err error
	for i := 0; i < nPipes; i++ {
		// Use os.Pipe and NOT net.Pipe.
		// The latter (based on io.Pipe) doesn't really do any I/O
		// on the Write, it just manipulates pointers (the slice)
		// and thus isn't useful when benchmarking since that
		// implementation is excessively cache friendly.
		readers[i], writers[i], err = os.Pipe()
		if err != nil {
			b.Fatalf("Failed to create pipe #%d: %v", i, err)
			return
		}
	}
	(&throughputTester{b: b, readers: readers, writers: writers}).Run()
}
