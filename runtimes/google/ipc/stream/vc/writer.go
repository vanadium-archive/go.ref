package vc

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"veyron.io/veyron/veyron/runtimes/google/lib/bqueue"
	"veyron.io/veyron/veyron/runtimes/google/lib/iobuf"
	vsync "veyron.io/veyron/veyron/runtimes/google/lib/sync"
)

var errWriterClosed = errors.New("attempt to call Write on Flow that has been Closed")

// writer implements the io.Writer and SetWriteDeadline interfaces for Flow.
type writer struct {
	MTU            int              // Maximum size (in bytes) of each slice Put into Sink.
	Sink           bqueue.Writer    // Buffer queue writer where data from Write is sent as iobuf.Slice objects.
	Alloc          *iobuf.Allocator // Allocator for iobuf.Slice objects. GUARDED_BY(mu)
	SharedCounters *vsync.Semaphore // Semaphore hosting counters shared by all flows over a VC.

	mu        sync.Mutex      // Guards call to Writes
	wroteOnce bool            // GUARDED_BY(mu)
	isClosed  bool            // GUARDED_BY(mu)
	closed    chan struct{}   // GUARDED_BY(mu)
	deadline  <-chan struct{} // GUARDED_BY(mu)

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
	}
}

// Shutdown closes the writer and discards any queued up write buffers, i.e.,
// the bqueue.Get call will not see the buffers queued up at this writer.
// If removeWriter is true the writer will also be removed entirely from the
// bqueue, otherwise the now empty writer will eventually be returned by
// bqueue.Get.
func (w *writer) Shutdown(removeWriter bool) {
	w.Sink.Shutdown(removeWriter)
	w.finishClose()
}

// Close closes the writer without discarding any queued up write buffers.
func (w *writer) Close() {
	w.Sink.Close()
	w.finishClose()
}

func (w *writer) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.isClosed
}

func (w *writer) Closed() <-chan struct{} {
	return w.closed
}

func (w *writer) finishClose() {
	// IsClosed() and Closed() indicate that the writer is closed before
	// finishClose() completes. This is safe because Alloc and shared counters
	// are guarded, and are not accessed elsewhere after w.closed is closed.
	w.mu.Lock()
	// finishClose() is idempotent, but Go's builtin close is not.
	if !w.isClosed {
		w.isClosed = true
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
		return 0, errWriterClosed
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
					return 0, timeoutError{}
				}
				return 0, fmt.Errorf("failed to get quota from receive buffers shared by all new flows on a VC: %v", err)
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
				return written, timeoutError{}
			case bqueue.ErrWriterIsClosed:
				return written, errWriterClosed
			default:
				return written, fmt.Errorf("bqueue.Writer.Put failed: %v", err)
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
	defer w.mu.Unlock()
	w.deadline = deadline
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
