package vc

import (
	"testing"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
)

type noopFlow struct{}

// net.Conn methods
func (*noopFlow) Read([]byte) (int, error)        { return 0, nil }
func (*noopFlow) Write([]byte) (int, error)       { return 0, nil }
func (*noopFlow) Close() error                    { return nil }
func (*noopFlow) IsClosed() bool                  { return false }
func (*noopFlow) Closed() <-chan struct{}         { return nil }
func (*noopFlow) Cancel()                         {}
func (*noopFlow) LocalEndpoint() naming.Endpoint  { return nil }
func (*noopFlow) RemoteEndpoint() naming.Endpoint { return nil }

// Other stream.Flow methods
func (*noopFlow) LocalPrincipal() security.Principal  { return nil }
func (*noopFlow) LocalBlessings() security.Blessings  { return nil }
func (*noopFlow) RemoteBlessings() security.Blessings { return nil }
func (*noopFlow) SetDeadline(<-chan struct{})         {}

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
