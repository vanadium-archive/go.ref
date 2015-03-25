// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

type serverCall struct {
	rpc.StreamServerCall
	context *context.T
}

func fakeCall(provider, user security.Principal) rpc.StreamServerCall {
	secCall := security.NewCall(&security.CallParams{
		LocalPrincipal:  provider,
		LocalBlessings:  blessSelf(provider, "provider"),
		RemoteBlessings: blessSelf(user, "self-signed-user"),
	})
	ctx, _ := context.RootContext()
	ctx = security.SetCall(ctx, secCall)
	return &serverCall{context: ctx}
}

func (c *serverCall) Context() *context.T { return c.context }

func blessSelf(p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return b
}

func newCaveat(c security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return c
}
