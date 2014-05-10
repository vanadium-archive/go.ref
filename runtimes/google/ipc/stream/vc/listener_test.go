package vc

import (
	"net"
	"testing"
	"time"

	"veyron2/security"
)

type noopFlow struct{}

// net.Conn methods
func (*noopFlow) Read([]byte) (int, error)           { return 0, nil }
func (*noopFlow) Write([]byte) (int, error)          { return 0, nil }
func (*noopFlow) Close() error                       { return nil }
func (*noopFlow) IsClosed() bool                     { return false }
func (*noopFlow) Closed() <-chan struct{}            { return nil }
func (*noopFlow) Cancel()                            {}
func (*noopFlow) LocalAddr() net.Addr                { return nil }
func (*noopFlow) RemoteAddr() net.Addr               { return nil }
func (*noopFlow) SetDeadline(t time.Time) error      { return nil }
func (*noopFlow) SetReadDeadline(t time.Time) error  { return nil }
func (*noopFlow) SetWriteDeadline(t time.Time) error { return nil }

// Other stream.Flow methods
func (*noopFlow) LocalID() security.PublicID  { return security.FakePublicID("test") }
func (*noopFlow) RemoteID() security.PublicID { return security.FakePublicID("test") }

func TestListener(t *testing.T) {
	ln := newListener()
	f1, f2 := &noopFlow{}, &noopFlow{}

	if err := ln.Enqueue(f1); err != nil {
		t.Error(err)
	}
	if err := ln.Enqueue(f2); err != nil {
		t.Error(err)
	}
	if f, err := ln.Accept(); f != f1 || err != nil {
		t.Errorf("Got (%p, %v) want (%p, nil)", f, err, f1)
	}
	if f, err := ln.Accept(); f != f2 || err != nil {
		t.Errorf("Got (%p, %v) want (%p, nil)", f, err, f2)
	}
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	// Close-ing multiple times is fine.
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	if err := ln.Enqueue(f1); err != errListenerClosed {
		t.Error(err)
	}
	if f, err := ln.Accept(); f != nil || err != errListenerClosed {
		t.Errorf("Accept returned (%p, %v) wanted (nil, %v)", f, err, errListenerClosed)
	}
}
