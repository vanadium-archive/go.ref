// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package server

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

type mojoServerCall struct {
	sec    security.Call
	srv    rpc.Server
	suffix string
}

// TODO(sadovsky): Synthesize endpoints and discharges as needed.
func newMojoServerCall(ctx *context.T, srv rpc.Server, suffix string, method rpc.MethodDesc) rpc.ServerCall {
	p := v23.GetPrincipal(ctx)
	// HACK: For now, we set the remote (client, i.e. Mojo app) blessing to be the
	// same as the local (server, i.e. Syncbase Mojo service) blessing.
	// TODO(sadovsky): Eliminate this hack.
	blessings, _ := p.BlessingStore().Default()
	return &mojoServerCall{
		sec: security.NewCall(&security.CallParams{
			Method:          method.Name,
			MethodTags:      method.Tags,
			Suffix:          suffix,
			LocalPrincipal:  p,
			LocalBlessings:  blessings,
			RemoteBlessings: blessings,
		}),
		srv:    srv,
		suffix: suffix,
	}
}

var _ rpc.ServerCall = (*mojoServerCall)(nil)

func (call *mojoServerCall) Security() security.Call {
	return call.sec
}

func (call *mojoServerCall) Suffix() string {
	return call.suffix
}

func (call *mojoServerCall) LocalEndpoint() naming.Endpoint {
	return call.sec.LocalEndpoint()
}

func (call *mojoServerCall) RemoteEndpoint() naming.Endpoint {
	return call.sec.RemoteEndpoint()
}

func (call *mojoServerCall) GrantedBlessings() security.Blessings {
	return security.Blessings{}
}

func (call *mojoServerCall) Server() rpc.Server {
	return call.srv
}
