// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"time"

	"v.io/v23/naming"
	"v.io/v23/security"

	"v.io/x/ref/runtime/internal/rpc/stream"
)

type flow struct {
	backingVC
	*reader
	*writer
	channelTimeout time.Duration
}

type backingVC interface {
	LocalEndpoint() naming.Endpoint
	RemoteEndpoint() naming.Endpoint

	LocalPrincipal() security.Principal
	LocalBlessings() security.Blessings
	RemoteBlessings() security.Blessings
	LocalDischarges() map[string]security.Discharge
	RemoteDischarges() map[string]security.Discharge

	VCDataCache() stream.VCDataCache
}

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
