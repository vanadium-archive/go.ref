// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"strings"
	"testing"

	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/ref/profiles/internal/rpc/stream"
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
func (*noopFlow) LocalPrincipal() security.Principal              { return nil }
func (*noopFlow) LocalBlessings() security.Blessings              { return security.Blessings{} }
func (*noopFlow) RemoteBlessings() security.Blessings             { return security.Blessings{} }
func (*noopFlow) LocalDischarges() map[string]security.Discharge  { return nil }
func (*noopFlow) RemoteDischarges() map[string]security.Discharge { return nil }
func (*noopFlow) SetDeadline(<-chan struct{})                     {}
func (*noopFlow) VCDataCache() stream.VCDataCache                 { return nil }

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
		t.Errorf("Got (%v, %v) want (%v, nil)", f, err, f1)
	}
	if f, err := ln.Accept(); f != f2 || err != nil {
		t.Errorf("Got (%v, %v) want (%v, nil)", f, err, f2)
	}
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	// Close-ing multiple times is fine.
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	if err := ln.Enqueue(f1); verror.ErrorID(err) != stream.ErrBadState.ID || !strings.Contains(err.Error(), "closed") {
		t.Error(err)
	}
	if f, err := ln.Accept(); f != nil || verror.ErrorID(err) != stream.ErrBadState.ID || !strings.Contains(err.Error(), "closed") {
		t.Errorf("Accept returned (%v, %v) wanted (nil, %v)", f, err, errListenerClosed)
	}
}
