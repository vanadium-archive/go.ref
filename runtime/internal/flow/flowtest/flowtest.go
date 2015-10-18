// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flowtest

import (
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/security"
)

// Pipe returns a connection pair dialed on against a listener using
// the given network and address.
func Pipe(t *testing.T, ctx *context.T, network, address string) (dialed, accepted flow.Conn) {
	local, _ := flow.RegisteredProtocol(network)
	if local == nil {
		t.Fatalf("No registered protocol %s", network)
	}
	l, err := local.Listen(ctx, network, address)
	if err != nil {
		t.Fatal(err)
	}
	d, err := local.Dial(ctx, l.Addr().Network(), l.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a, err := l.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	return d, a
}

type AllowAllPeersAuthorizer struct{}

func (AllowAllPeersAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) ([]string, []security.RejectedBlessing, error) {
	return nil, nil, nil
}

func (AllowAllPeersAuthorizer) BlessingsForPeer(ctx *context.T, _ []string) (
	security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}
