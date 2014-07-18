package vc

import (
	"net"
	"testing"
	"time"

	isecurity "veyron/runtimes/google/security"

	"veyron2/naming"
	"veyron2/security"
)

var testID = newID("test")

type noopFlow struct{}

func newID(name string) security.PrivateID {
	id, err := isecurity.NewPrivateID(name)
	if err != nil {
		panic(err)
	}
	return id
}

// net.Conn methods
func (*noopFlow) Read([]byte) (int, error)           { return 0, nil }
func (*noopFlow) Write([]byte) (int, error)          { return 0, nil }
func (*noopFlow) Close() error                       { return nil }
func (*noopFlow) IsClosed() bool                     { return false }
func (*noopFlow) Closed() <-chan struct{}            { return nil }
func (*noopFlow) Cancel()                            {}
func (*noopFlow) LocalEndpoint() naming.Endpoint     { return nil }
func (*noopFlow) RemoteEndpoint() naming.Endpoint    { return nil }
func (*noopFlow) LocalAddr() net.Addr                { return nil }
func (*noopFlow) RemoteAddr() net.Addr               { return nil }
func (*noopFlow) SetDeadline(t time.Time) error      { return nil }
func (*noopFlow) SetReadDeadline(t time.Time) error  { return nil }
func (*noopFlow) SetWriteDeadline(t time.Time) error { return nil }

// Other stream.Flow methods
func (*noopFlow) LocalID() security.PublicID  { return testID.PublicID() }
func (*noopFlow) RemoteID() security.PublicID { return testID.PublicID() }

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
