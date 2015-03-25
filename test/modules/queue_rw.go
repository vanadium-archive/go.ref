// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules

import (
	"bytes"
	"io"
	"sync"
)

// queueRW implements a ReadWriteCloser backed by an unbounded in-memory
// buffer.
type queueRW struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newRW() io.ReadWriteCloser {
	q := &queueRW{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *queueRW) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	defer q.cond.Broadcast()
	q.closed = true
	return nil
}

func (q *queueRW) Read(p []byte) (n int, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.buf.Len() == 0 && !q.closed {
		q.cond.Wait()
	}
	return q.buf.Read(p)
}

func (q *queueRW) Write(p []byte) (n int, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	defer q.cond.Broadcast()
	return q.buf.Write(p)
}
