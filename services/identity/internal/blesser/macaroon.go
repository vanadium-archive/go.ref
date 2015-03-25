// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"fmt"
	"time"

	"v.io/x/ref/services/identity"
	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/util"

	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vom"
)

type macaroonBlesser struct {
	key []byte
}

// NewMacaroonBlesserServer provides an identity.MacaroonBlesser Service that generates blessings
// after unpacking a BlessingMacaroon.
func NewMacaroonBlesserServer(key []byte) identity.MacaroonBlesserServerStub {
	return identity.MacaroonBlesserServer(&macaroonBlesser{key})
}

func (b *macaroonBlesser) Bless(call rpc.ServerCall, macaroon string) (security.Blessings, error) {
	var empty security.Blessings
	inputs, err := util.Macaroon(macaroon).Decode(b.key)
	if err != nil {
		return empty, err
	}
	var m oauth.BlessingMacaroon
	if err := vom.Decode(inputs, &m); err != nil {
		return empty, err
	}
	if time.Now().After(m.Creation.Add(time.Minute * 5)) {
		return empty, fmt.Errorf("macaroon has expired")
	}
	if call.LocalPrincipal() == nil {
		return empty, fmt.Errorf("server misconfiguration: no authentication happened")
	}
	if len(m.Caveats) == 0 {
		m.Caveats = []security.Caveat{security.UnconstrainedUse()}
	}
	return call.LocalPrincipal().Bless(call.RemoteBlessings().PublicKey(), call.LocalBlessings(), m.Name, m.Caveats[0], m.Caveats[1:]...)
}
