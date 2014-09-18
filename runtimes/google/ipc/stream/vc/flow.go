package vc

import (
	"net"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
)

type flow struct {
	idHolder
	*reader
	*writer
	localEndpoint, remoteEndpoint naming.Endpoint
}

type idHolder interface {
	LocalID() security.PublicID
	RemoteID() security.PublicID
}

func (f *flow) LocalEndpoint() naming.Endpoint  { return f.localEndpoint }
func (f *flow) RemoteEndpoint() naming.Endpoint { return f.remoteEndpoint }

// implement net.Conn
func (f *flow) LocalAddr() net.Addr  { return f.localEndpoint }
func (f *flow) RemoteAddr() net.Addr { return f.remoteEndpoint }
func (f *flow) Close() error {
	f.reader.Close()
	f.writer.Close()
	return nil
}

// SetDeadline sets a deadline on the flow. The flow will be cancelled if it
// is not closed by the specified deadline.
// A zero deadline (time.Time.IsZero) implies that no cancellation is desired.
func (f *flow) SetDeadline(t time.Time) error {
	if err := f.SetReadDeadline(t); err != nil {
		return err
	}
	if err := f.SetWriteDeadline(t); err != nil {
		return err
	}
	return nil
}

// Shutdown closes the flow and discards any queued up write buffers.
// This is appropriate when the flow has been closed by the remote end.
func (f *flow) Shutdown() {
	f.reader.Close()
	f.writer.Shutdown(true)
}

// Cancel closes the flow and discards any queued up write buffers.
// This is appropriate when the flow is being cancelled locally.
func (f *flow) Cancel() {
	f.reader.Close()
	f.writer.Shutdown(false)
}
