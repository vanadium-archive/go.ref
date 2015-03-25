// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"v.io/x/ref/profiles/internal/lib/iobuf"
	vsync "v.io/x/ref/profiles/internal/lib/sync"
	"v.io/x/ref/profiles/internal/lib/upcqueue"
)

// readHandler is the interface used by the reader to notify other components
// of the number of bytes returned in Read calls.
type readHandler interface {
	HandleRead(bytes uint)
}

// reader implements the io.Reader and SetReadDeadline interfaces for a Flow,
// backed by iobuf.Slice objects read from a upcqueue.
type reader struct {
	handler    readHandler
	src        *upcqueue.T
	mu         sync.Mutex
	buf        *iobuf.Slice    // GUARDED_BY(mu)
	deadline   <-chan struct{} // GUARDED_BY(mu)
	totalBytes uint32
}

func newReader(h readHandler) *reader {
	return &reader{handler: h, src: upcqueue.New()}
}

func (r *reader) Close() {
	r.src.Close()
}

func (r *reader) Read(b []byte) (int, error) {
	// net.Conn requires that all methods be invokable by multiple
	// goroutines simultaneously. Read calls are serialized to ensure
	// contiguous chunks of data are provided from each Read call.
	r.mu.Lock()
	n, err := r.readLocked(b)
	r.mu.Unlock()
	atomic.AddUint32(&r.totalBytes, uint32(n))
	if n > 0 {
		r.handler.HandleRead(uint(n))
	}
	return n, err
}

func (r *reader) readLocked(b []byte) (int, error) {
	if r.buf == nil {
		slice, err := r.src.Get(r.deadline)
		if err != nil {
			switch err {
			case upcqueue.ErrQueueIsClosed:
				return 0, io.EOF
			case vsync.ErrCanceled:
				// As per net.Conn.Read specification
				return 0, timeoutError{}
			default:
				return 0, fmt.Errorf("upcqueue.Get failed: %v", err)
			}
		}
		r.buf = slice.(*iobuf.Slice)
	}
	copied := 0
	for r.buf.Size() <= len(b) {
		n := copy(b, r.buf.Contents)
		copied += n
		b = b[n:]
		r.buf.Release()
		r.buf = nil

		slice, err := r.src.TryGet()
		if err != nil {
			return copied, nil
		}
		r.buf = slice.(*iobuf.Slice)
	}
	n := copy(b, r.buf.Contents)
	r.buf.TruncateFront(uint(n))
	copied += n
	return copied, nil
}

func (r *reader) SetDeadline(deadline <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deadline = deadline
}

func (r *reader) BytesRead() uint32 {
	return atomic.LoadUint32(&r.totalBytes)
}

func (r *reader) Put(slice *iobuf.Slice) error {
	return r.src.Put(slice)
}

// timeoutError implements net.Error with Timeout returning true.
type timeoutError struct{}

func (t timeoutError) Error() string   { return "deadline exceeded" }
func (t timeoutError) Timeout() bool   { return true }
func (t timeoutError) Temporary() bool { return false }
