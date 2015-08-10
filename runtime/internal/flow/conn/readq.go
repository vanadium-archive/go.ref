// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"io"
	"sync"

	"v.io/v23/context"
)

type readq struct {
	mu   sync.Mutex
	bufs [][]byte
	b, e int

	size      int
	nbufs     int
	toRelease int
	notify    chan struct{}
}

const initialReadqBufferSize = 10

func newReadQ() *readq {
	return &readq{
		bufs:      make([][]byte, initialReadqBufferSize),
		notify:    make(chan struct{}, 1),
		toRelease: defaultBufferSize,
	}
}

func (r *readq) put(ctx *context.T, bufs [][]byte) error {
	l := 0
	for _, b := range bufs {
		l += len(b)
	}
	if l == 0 {
		return nil
	}

	defer r.mu.Unlock()
	r.mu.Lock()
	if r.e == -1 {
		// The flow has already closed.  Simply drop the data.
		return nil
	}
	newSize := l + r.size
	if newSize > defaultBufferSize {
		return NewErrCounterOverflow(ctx)
	}
	newBufs := r.nbufs + len(bufs)
	r.reserveLocked(newBufs)
	for _, b := range bufs {
		r.bufs[r.e] = b
		r.e = (r.e + 1) % len(r.bufs)
	}
	r.nbufs = newBufs
	if r.size == 0 {
		select {
		case r.notify <- struct{}{}:
		default:
		}
	}
	r.size = newSize
	return nil
}

func (r *readq) read(ctx *context.T, data []byte) (n int, release bool, err error) {
	defer r.mu.Unlock()
	r.mu.Lock()
	if err := r.waitLocked(ctx); err != nil {
		return 0, false, err
	}
	buf := r.bufs[r.b]
	n = copy(data, buf)
	buf = buf[n:]
	if len(buf) > 0 {
		r.bufs[r.b] = buf
	} else {
		r.nbufs -= 1
		r.b = (r.b + 1) % len(r.bufs)
	}
	r.size -= n
	r.toRelease += n
	return n, r.toRelease > defaultBufferSize/2, nil
}

func (r *readq) get(ctx *context.T) (out []byte, release bool, err error) {
	defer r.mu.Unlock()
	r.mu.Lock()
	if err := r.waitLocked(ctx); err != nil {
		return nil, false, err
	}
	out = r.bufs[r.b]
	r.b = (r.b + 1) % len(r.bufs)
	r.size -= len(out)
	r.nbufs -= 1
	r.toRelease += len(out)
	return out, r.toRelease > defaultBufferSize/2, nil
}

func (r *readq) waitLocked(ctx *context.T) (err error) {
	for r.size == 0 && err == nil {
		r.mu.Unlock()
		select {
		case _, ok := <-r.notify:
			if !ok {
				err = io.EOF
			}
		case <-ctx.Done():
			if r.size == 0 {
				err = ctx.Err()
			}
		}
		r.mu.Lock()
	}
	return
}

func (r *readq) close(ctx *context.T) {
	r.mu.Lock()
	if r.e != -1 {
		r.e = -1
		r.toRelease = 0
		close(r.notify)
	}
	r.mu.Unlock()
}

func (r *readq) reserveLocked(n int) {
	if n < len(r.bufs) {
		return
	}
	nb := make([][]byte, 2*n)
	copied := 0
	if r.e >= r.b {
		copied = copy(nb, r.bufs[r.b:r.e])
	} else {
		copied = copy(nb, r.bufs[r.b:])
		copied += copy(nb[copied:], r.bufs[:r.e])
	}
	r.bufs, r.b, r.e = nb, 0, copied
}

func (r *readq) release() (out int) {
	r.mu.Lock()
	out, r.toRelease = r.toRelease, 0
	r.mu.Unlock()
	return out
}
