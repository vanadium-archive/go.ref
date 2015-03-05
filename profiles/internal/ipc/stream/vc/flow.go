package vc

import (
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/x/ref/profiles/internal/ipc/stream"
)

type flow struct {
	authN // authentication information.
	*reader
	*writer
	localEndpoint, remoteEndpoint naming.Endpoint
	dataCache                     *dataCache
}

type authN interface {
	LocalPrincipal() security.Principal
	LocalBlessings() security.Blessings
	RemoteBlessings() security.Blessings
	LocalDischarges() map[string]security.Discharge
	RemoteDischarges() map[string]security.Discharge
}

func (f *flow) LocalEndpoint() naming.Endpoint  { return f.localEndpoint }
func (f *flow) RemoteEndpoint() naming.Endpoint { return f.remoteEndpoint }

func (f *flow) Close() error {
	f.reader.Close()
	f.writer.Close()
	return nil
}

// SetDeadline sets a deadline channel on the flow.  Reads and writes
// will be cancelled if the channel is closed.
func (f *flow) SetDeadline(deadline <-chan struct{}) {
	f.reader.SetDeadline(deadline)
	f.writer.SetDeadline(deadline)
}

// Shutdown closes the flow and discards any queued up write buffers.
// This is appropriate when the flow has been closed by the remote end.
func (f *flow) Shutdown() {
	f.reader.Close()
	f.writer.shutdown(true)
}

// Cancel closes the flow and discards any queued up write buffers.
// This is appropriate when the flow is being cancelled locally.
func (f *flow) Cancel() {
	f.reader.Close()
	f.writer.shutdown(false)
}

// VCDataCache returns the stream.VCDataCache object that allows information to be
// shared across the Flow's parent VC.
func (f *flow) VCDataCache() stream.VCDataCache {
	return f.dataCache
}
