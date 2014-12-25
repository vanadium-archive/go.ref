package vc

import (
	"bytes"
	"io"
	"net"
	"reflect"
	"testing"

	"v.io/veyron/veyron/runtimes/google/lib/bqueue"
	"v.io/veyron/veyron/runtimes/google/lib/bqueue/drrqueue"
	"v.io/veyron/veyron/runtimes/google/lib/iobuf"
	"v.io/veyron/veyron/runtimes/google/lib/sync"
)

// TestWrite is a very basic, easy to follow, but not very thorough test of the
// writer.  More thorough testing of flows (and implicitly the writer) is in
// vc_test.go.
func TestWrite(t *testing.T) {
	bq := drrqueue.New(128)
	defer bq.Close()

	bw, err := bq.NewWriter(0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	shared := sync.NewSemaphore()
	shared.IncN(4)

	w := newTestWriter(bw, shared)

	if n, err := w.Write([]byte("abcd")); n != 4 || err != nil {
		t.Errorf("Got (%d, %v) want (4, nil)", n, err)
	}

	// Should have used up 4 shared counters
	if err := shared.TryDecN(1); err != sync.ErrTryAgain {
		t.Errorf("Got %v want %v", err, sync.ErrTryAgain)
	}

	// Further Writes will block until some space has been released.
	w.Release(10)
	if n, err := w.Write([]byte("efghij")); n != 6 || err != nil {
		t.Errorf("Got (%d, %v) want (5, nil)", n, err)
	}
	// And the release should have returned to the shared counters set
	if err := shared.TryDecN(4); err != nil {
		t.Errorf("Got %v want %v", err, nil)
	}

	// Further writes will block since all 10 bytes (provided to NewWriter)
	// have been exhausted and Get hasn't been called on bq yet.
	deadline := make(chan struct{}, 0)
	w.SetDeadline(deadline)
	close(deadline)

	w.SetDeadline(deadline)
	if n, err := w.Write([]byte("k")); n != 0 || !isTimeoutError(err) {
		t.Errorf("Got (%d, %v) want (0, timeout error)", n, err)
	}

	w.Close()
	if w.BytesWritten() != 10 {
		t.Errorf("Got %d want %d", w.BytesWritten(), 10)
	}

	_, bufs, err := bq.Get(nil)
	var read bytes.Buffer
	for _, b := range bufs {
		read.Write(b.Contents)
		b.Release()
	}
	if g, w := read.String(), "abcdefghij"; g != w {
		t.Errorf("Got %q want %q", g, w)
	}
}

func TestCloseBeforeWrite(t *testing.T) {
	bq := drrqueue.New(128)
	defer bq.Close()

	bw, err := bq.NewWriter(0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	shared := sync.NewSemaphore()
	shared.IncN(4)

	w := newTestWriter(bw, shared)
	w.Close()

	if n, err := w.Write([]byte{1, 2}); n != 0 || err != errWriterClosed {
		t.Errorf("Got (%v, %v) want (0, %v)", n, err, errWriterClosed)
	}
}

func TestShutdownBeforeWrite(t *testing.T) {
	bq := drrqueue.New(128)
	defer bq.Close()

	bw, err := bq.NewWriter(0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	shared := sync.NewSemaphore()
	shared.IncN(4)

	w := newTestWriter(bw, shared)
	w.shutdown(true)

	if n, err := w.Write([]byte{1, 2}); n != 0 || err != io.EOF {
		t.Errorf("Got (%v, %v) want (0, %v)", n, err, io.EOF)
	}
}

func TestCloseDoesNotDiscardPendingWrites(t *testing.T) {
	bq := drrqueue.New(128)
	defer bq.Close()

	bw, err := bq.NewWriter(0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	shared := sync.NewSemaphore()
	shared.IncN(2)

	w := newTestWriter(bw, shared)
	data := []byte{1, 2}
	if n, err := w.Write(data); n != len(data) || err != nil {
		t.Fatalf("Got (%d, %v) want (%d, nil)", n, err, len(data))
	}
	w.Close()

	gbw, bufs, err := bq.Get(nil)
	if err != nil {
		t.Fatal(err)
	}
	if gbw != bw {
		t.Fatalf("Got %p want %p", gbw, bw)
	}
	if len(bufs) != 1 {
		t.Fatalf("Got %d bufs, want 1", len(bufs))
	}
	if !reflect.DeepEqual(bufs[0].Contents, data) {
		t.Fatalf("Got %v want %v", bufs[0].Contents, data)
	}
	if !gbw.IsDrained() {
		t.Fatal("Expected bqueue.Writer to be drained")
	}
}

func TestWriterCloseIsIdempotent(t *testing.T) {
	bq := drrqueue.New(128)
	defer bq.Close()

	bw, err := bq.NewWriter(0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	shared := sync.NewSemaphore()
	shared.IncN(1)
	w := newTestWriter(bw, shared)
	if n, err := w.Write([]byte{1}); n != 1 || err != nil {
		t.Fatalf("Got (%d, %v) want (1, nil)", n, err)
	}
	// Should have used up the shared counter.
	if err := shared.TryDec(); err != sync.ErrTryAgain {
		t.Fatalf("Got %v want %v", err, sync.ErrTryAgain)
	}
	w.Close()
	// The shared counter should have been returned
	if err := shared.TryDec(); err != nil {
		t.Fatalf("Got %v want nil", err)
	}
	// Closing again shouldn't affect the shared counters
	w.Close()
	if err := shared.TryDec(); err != sync.ErrTryAgain {
		t.Fatalf("Got %v want %v", err, sync.ErrTryAgain)
	}
}

func TestClosedChannel(t *testing.T) {
	bq := drrqueue.New(128)
	defer bq.Close()

	bw, err := bq.NewWriter(0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	shared := sync.NewSemaphore()
	shared.IncN(4)

	w := newTestWriter(bw, shared)
	go w.Close()
	<-w.Closed()

	if n, err := w.Write([]byte{1, 2}); n != 0 || err != errWriterClosed {
		t.Errorf("Got (%v, %v) want (0, %v)", n, err, errWriterClosed)
	}
}

func newTestWriter(bqw bqueue.Writer, shared *sync.Semaphore) *writer {
	alloc := iobuf.NewAllocator(iobuf.NewPool(0), 0)
	return newWriter(16, bqw, alloc, shared)
}

func isTimeoutError(err error) bool {
	neterr, ok := err.(net.Error)
	return ok && neterr.Timeout()
}
