package modules

import (
	"io"
	"sync"

	// TODO(caprita): Move upcqueue into veyron/lib.
	"v.io/core/veyron/runtimes/google/lib/upcqueue"
)

// queueRW implements a ReadWriteCloser backed by an unbounded in-memory
// producer-consumer queue.
type queueRW struct {
	sync.Mutex
	q        *upcqueue.T
	buf      []byte
	buffered int
	cancel   chan struct{}
}

func newRW() io.ReadWriteCloser {
	return &queueRW{q: upcqueue.New(), cancel: make(chan struct{})}
}

func (q *queueRW) Close() error {
	// We use an empty message to signal EOF to the reader.
	_, err := q.Write([]byte{})
	return err
}

func (q *queueRW) Read(p []byte) (n int, err error) {
	q.Lock()
	defer q.Unlock()
	for q.buffered == 0 {
		elem, err := q.q.Get(q.cancel)
		if err != nil {
			return 0, io.EOF
		}
		s := elem.(string)
		b := []byte(s)
		if len(b) == 0 {
			close(q.cancel)
			return 0, io.EOF
		}
		q.buf, q.buffered = b, len(b)
	}
	copied := copy(p, q.buf[:q.buffered])
	q.buf = q.buf[copied:]
	q.buffered -= copied
	return copied, nil
}

func (q *queueRW) Write(p []byte) (n int, err error) {
	if err := q.q.Put(string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}
