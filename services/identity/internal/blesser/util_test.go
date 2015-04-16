// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"v.io/v23/context"
	"v.io/v23/security"
)

func fakeContext(provider, user security.Principal) *context.T {
	secCall := security.NewCall(&security.CallParams{
		LocalPrincipal:  provider,
		LocalBlessings:  blessSelf(provider, "provider"),
		RemoteBlessings: blessSelf(user, "self-signed-user"),
	})
	ctx, _ := context.RootContext()
	return security.SetCall(ctx, secCall)
}

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
