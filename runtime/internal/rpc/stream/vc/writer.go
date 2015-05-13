// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"io"
	"sync"
	"sync/atomic"

	"v.io/v23/verror"

	"v.io/x/ref/runtime/internal/lib/bqueue"
	"v.io/x/ref/runtime/internal/lib/iobuf"
	vsync "v.io/x/ref/runtime/internal/lib/sync"
	"v.io/x/ref/runtime/internal/rpc/stream"
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errWriterClosed     = reg(".errWriterClosed", "attempt to call Write on Flow that has been Closed")
	errBQueuePutFailed  = reg(".errBqueuePutFailed", "bqueue.Writer.Put failed{:3}")
	errFailedToGetQuota = reg(".errFailedToGetQuota", "failed to get quota from receive buffers shared by all new flows on a VC{:3}")
	errCanceled         = reg(".errCanceled", "underlying queues canceled")
)

// writer implements the io.Writer and SetWriteDeadline interfaces for Flow.
type writer struct {
	MTU            int              // Maximum size (in bytes) of each slice Put into Sink.
	Sink           bqueue.Writer    // Buffer queue writer where data from Write is sent as iobuf.Slice objects.
	Alloc          *iobuf.Allocator // Allocator for iobuf.Slice objects. GUARDED_BY(mu)
	SharedCounters *vsync.Semaphore // Semaphore hosting counters shared by all flows over a VC.

	mu         sync.Mutex      // Guards call to Writes
	wroteOnce  bool            // GUARDED_BY(mu)
	isClosed   bool            // GUARDED_BY(mu)
	closeError error           // GUARDED_BY(mu)
	closed     chan struct{}   // GUARDED_BY(mu)
	deadline   <-chan struct{} // GUARDED_BY(mu)

	// Total number of bytes filled in by all Write calls on this writer.
	// Atomic operations are used to manipulate it.
	totalBytes uint32

	// Accounting for counters borrowed from the shared pool.
	muSharedCountersBorrowed sync.Mutex
	sharedCountersBorrowed   int // GUARDED_BY(muSharedCountersBorrowed)
}

func newWriter(mtu int, sink bqueue.Writer, alloc *iobuf.Allocator, counters *vsync.Semaphore) *writer {
	return &writer{
		MTU:            mtu,
		Sink:           sink,
		Alloc:          alloc,
		SharedCounters: counters,
		closed:         make(chan struct{}),
		closeError:     verror.New(errWriterClosed, nil),
	}
}

// Shutdown closes the writer and discards any queued up write buffers, i.e.,
// the bqueue.Get call will not see the buffers queued up at this writer.
// If removeWriter is true the writer will also be removed entirely from the
// bqueue, otherwise the now empty writer will eventually be returned by
// bqueue.Get.
func (w *writer) shutdown(removeWriter bool) {
	w.Sink.Shutdown(removeWriter)
	w.finishClose(true)
}

// Close closes the writer without discarding any queued up write buffers.
func (w *writer) Close() {
	w.Sink.Close()
	w.finishClose(false)
}

func (w *writer) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.isClosed
}

func (w *writer) Closed() <-chan struct{} {
	return w.closed
}

func (w *writer) finishClose(remoteShutdown bool) {
	// IsClosed() and Closed() indicate that the writer is closed before
	// finishClose() completes. This is safe because Alloc and shared counters
	// are guarded, and are not accessed elsewhere after w.closed is closed.
	w.mu.Lock()
	// finishClose() is idempotent, but Go's builtin close is not.
	if !w.isClosed {
		w.isClosed = true
		if remoteShutdown {
			w.closeError = io.EOF
		}
		close(w.closed)
	}

	w.Alloc.Release()
	w.mu.Unlock()

	w.muSharedCountersBorrowed.Lock()
	w.SharedCounters.IncN(uint(w.sharedCountersBorrowed))
	w.sharedCountersBorrowed = 0
	w.muSharedCountersBorrowed.Unlock()
}

// Write implements the Write call for a Flow.
//
// Flow control is achieved using receive buffers (aka counters), wherein the
// receiving end sends out the number of bytes that it is willing to read. To
// avoid an additional round-trip for the creation of new flows, the very first
// write of a new flow borrows counters from a shared pool.
func (w *writer) Write(b []byte) (int, error) {
	written := 0
	// net.Conn requires that multiple goroutines be able to invoke methods
	// simulatenously.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.isClosed {
		if w.closeError == io.EOF {
			return 0, io.EOF
		}
		return 0, verror.New(stream.ErrBadState, nil, w.closeError)
	}

	for len(b) > 0 {
		n := len(b)
		if n > w.MTU {
			n = w.MTU
		}
		if !w.wroteOnce && w.SharedCounters != nil {
			w.wroteOnce = true
			if n > MaxSharedBytes {
				n = MaxSharedBytes
			}
			if err := w.SharedCounters.DecN(uint(n), w.deadline); err != nil {
				if err == vsync.ErrCanceled {
					return 0, stream.NewNetError(verror.New(stream.ErrNetwork, nil, verror.New(errCanceled, nil)), true, false)
				}
				return 0, verror.New(stream.ErrNetwork, nil, verror.New(errFailedToGetQuota, nil, err))
			}
			w.muSharedCountersBorrowed.Lock()
			w.sharedCountersBorrowed = n
			w.muSharedCountersBorrowed.Unlock()
			w.Sink.Release(n)
		}
		slice := w.Alloc.Copy(b[:n])
		if err := w.Sink.Put(slice, w.deadline); err != nil {
			slice.Release()
			atomic.AddUint32(&w.totalBytes, uint32(written))
			switch err {
			case bqueue.ErrCancelled, vsync.ErrCanceled:
				return written, stream.NewNetError(verror.New(stream.ErrNetwork, nil, verror.New(errCanceled, nil)), true, false)
			case bqueue.ErrWriterIsClosed:
				return written, verror.New(stream.ErrBadState, nil, verror.New(errWriterClosed, nil))
			default:
				return written, verror.New(stream.ErrNetwork, nil, verror.New(errBQueuePutFailed, nil, err))
			}
		}
		written += n
		b = b[n:]
	}
	atomic.AddUint32(&w.totalBytes, uint32(written))
	return written, nil
}

func (w *writer) SetDeadline(deadline <-chan struct{}) {
	w.mu.Lock()
	w.deadline = deadline
	w.mu.Unlock()
}

// Release allows the next 'bytes' of data to be removed from the buffer queue
// writer and passed to bqueue.Get.
func (w *writer) Release(bytes int) {
	w.muSharedCountersBorrowed.Lock()
	switch {
	case w.sharedCountersBorrowed == 0:
		w.Sink.Release(bytes)
	case w.sharedCountersBorrowed >= bytes:
		w.SharedCounters.IncN(uint(bytes))
		w.sharedCountersBorrowed -= bytes
	default:
		w.SharedCounters.IncN(uint(w.sharedCountersBorrowed))
		w.Sink.Release(bytes - w.sharedCountersBorrowed)
		w.sharedCountersBorrowed = 0
	}
	w.muSharedCountersBorrowed.Unlock()
}

func (w *writer) BytesWritten() uint32 {
	return atomic.LoadUint32(&w.totalBytes)
}
