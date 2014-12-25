package vc

import (
	"io"
	"net"
	"reflect"
	"testing"
	"testing/quick"

	"v.io/veyron/veyron/runtimes/google/lib/iobuf"
)

type testReadHandler struct{ items []uint }

func (t *testReadHandler) HandleRead(bytes uint) {
	t.items = append(t.items, bytes)
}

func TestRead(t *testing.T) {
	l := &testReadHandler{}
	r := newReader(l)
	input := []byte("abcdefghijklmnopqrstuvwxyzABCDE") // 31 bytes total
	start := 0
	// Produce data to read, adding elements to the underlying upcqueue
	// with a geometric progression of 2.
	for n := 1; start < len(input); n *= 2 {
		if err := r.Put(iobuf.NewSlice(input[start : start+n])); err != nil {
			t.Fatalf("Put(start=%d, n=%d) failed: %v", start, n, err)
		}
		start = start + n
	}

	var output [31]byte
	start = 0
	// Read with geometric progression of 1/2.
	for n := 16; start < len(output); n /= 2 {
		if m, err := r.Read(output[start : start+n]); err != nil || m != n {
			t.Errorf("Read returned (%d, %v) want (%d, nil)", m, err, n)
		}
		if m := l.items[len(l.items)-1]; m != uint(n) {
			t.Errorf("Read notified %d but should have notified %d bytes", m, n)
		}
		start = start + n
	}
	if got, want := string(output[:]), string(input); got != want {
		t.Errorf("Got %q want %q", got, want)
	}

	r.Close()
	if n, err := r.Read(output[:]); n != 0 || err != io.EOF {
		t.Errorf("Got (%d, %v) want (0, nil)", n, err)
	}
}

func TestReadRandom(t *testing.T) {
	f := func(data [][]byte) bool {
		r := newReader(&testReadHandler{})
		// Use an empty slice (as opposed to a nil-slice) so that the
		// reflect.DeepEqual call below succeeds when data is
		// [][]byte{}.
		written := make([]byte, 0)
		for _, d := range data {
			if err := r.Put(iobuf.NewSlice(d)); err != nil {
				t.Error(err)
				return false
			}
			written = append(written, d...)
		}
		read := make([]byte, len(written))
		buf := read
		r.Close()
		for {
			n, err := r.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return false
			}
			buf = buf[n:]
		}
		return reflect.DeepEqual(written, read) && int(r.BytesRead()) == len(written)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestReadDeadline(t *testing.T) {
	l := &testReadHandler{}
	r := newReader(l)
	defer r.Close()

	deadline := make(chan struct{}, 0)
	r.SetDeadline(deadline)
	close(deadline)

	var buf [1]byte
	n, err := r.Read(buf[:])
	neterr, ok := err.(net.Error)
	if n != 0 || err == nil || !ok || !neterr.Timeout() {
		t.Errorf("Expected read to fail with net.Error.Timeout, got (%d, %v)", n, err)
	}
	if len(l.items) != 0 {
		t.Errorf("Expected no reads, but notified of reads: %v", l.items)
	}
}
