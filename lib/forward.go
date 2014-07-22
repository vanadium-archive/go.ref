package lib

import (
	"fmt"
	"io"
	"net"
)

type sender interface {
	Send([]uint8) error
}
type receiver interface {
	Advance() bool

	Value() []uint8

	Err() error
}

// stream is the interface common to TunnelForwardStream and TunnelServiceForwardStream.
type stream interface {
	sender
	receiver
}

// Forward forwards data read from net.Conn to a TunnelForwardStream or a TunnelServiceForwardStream.
func Forward(conn net.Conn, stream stream) error {
	defer conn.Close()
	// Both conn2stream and stream2conn will write to the channel exactly
	// once.
	// Forward reads from the channel exactly once.
	// A buffered channel is used to prevent the other write to the channel
	// from blocking.
	done := make(chan error, 1)
	go conn2stream(conn, stream, done)
	go stream2conn(stream, conn, done)
	return <-done
}

func conn2stream(r io.Reader, s sender, done chan error) {
	var buf [2048]byte
	for {
		n, err := r.Read(buf[:])
		if err == io.EOF {
			done <- nil
			return
		}
		if err != nil {
			done <- err
			return
		}
		if err := s.Send(buf[:n]); err != nil {
			done <- err
			return
		}
	}
}

func stream2conn(r receiver, w io.Writer, done chan error) {
	for r.Advance() {
		buf := r.Value()

		if n, err := w.Write(buf); n != len(buf) || err != nil {
			done <- fmt.Errorf("conn.Write returned (%d, %v) want (%d, nil)", n, err, len(buf))
			return
		}
	}
	done <- r.Err()
}
