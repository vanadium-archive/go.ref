// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"bytes"
	"encoding/hex"
	"io"
	"sync"
)

// TypeReader implements io.Reader but allows changing the underlying buffer.
// This is useful for merging discrete messages that are part of the same flow.
type TypeReader struct {
	buf      bytes.Buffer
	mu       sync.Mutex
	isClosed bool
	cond     *sync.Cond
}

func NewTypeReader() *TypeReader {
	reader := &TypeReader{}
	reader.cond = sync.NewCond(&reader.mu)
	return reader
}

func (r *TypeReader) Add(data string) error {
	binBytes, err := hex.DecodeString(data)
	if err != nil {
		return err
	}
	r.mu.Lock()
	_, err = r.buf.Write(binBytes)
	r.mu.Unlock()
	r.cond.Signal()
	return err
}

func (r *TypeReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		if r.buf.Len() > 0 {
			return r.buf.Read(p)
		}
		if r.isClosed {
			return 0, io.EOF
		}
		r.cond.Wait()
	}

}

func (r *TypeReader) Close() {
	r.mu.Lock()
	r.isClosed = true
	r.mu.Unlock()
	r.cond.Broadcast()
}
