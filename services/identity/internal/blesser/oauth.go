// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"fmt"
	"strings"
	"time"

	"v.io/x/ref/services/identity"
	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/revocation"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

type oauthBlesser struct {
	oauthProvider      oauth.OAuthProvider
	authcodeClient     struct{ ID, Secret string }
	accessTokenClients []oauth.AccessTokenClient
	duration           time.Duration
	dischargerLocation string
	revocationManager  revocation.RevocationManager
}

// OAuthBlesserParams represents all the parameters provided to NewOAuthBlesserServer
type OAuthBlesserParams struct {
	// The OAuth provider that must have issued the access tokens accepted by ths service.
	OAuthProvider oauth.OAuthProvider
	// The OAuth client IDs and names for the clients of the BlessUsingAccessToken RPCs.
	AccessTokenClients []oauth.AccessTokenClient
	// The object name of the discharger service. If this is empty then revocation caveats will not be granted.
	DischargerLocation string
	// The revocation manager that generates caveats and manages revocation.
	RevocationManager revocation.RevocationManager
	// The duration for which blessings will be valid. (Used iff RevocationManager is nil).
	BlessingDuration time.Duration
}

// NewOAuthBlesserServer provides an identity.OAuthBlesserService that uses OAuth2
// access tokens to obtain the username of a client and provide blessings with that
// name.
//
// Blessings generated by this service carry a third-party revocation caveat if a
// RevocationManager is specified by the params or they carry an ExpiryCaveat that
// expires after the duration specified by the params.
func NewOAuthBlesserServer(p OAuthBlesserParams) identity.OAuthBlesserServerStub {
	return identity.OAuthBlesserServer(&oauthBlesser{
		oauthProvider:      p.OAuthProvider,
		duration:           p.BlessingDuration,
		dischargerLocation: p.DischargerLocation,
		revocationManager:  p.RevocationManager,
		accessTokenClients: p.AccessTokenClients,
	})
}

func (b *oauthBlesser) BlessUsingAccessToken(ctx *context.T, call rpc.ServerCall, accessToken string) (security.Blessings, string, error) {
	var noblessings security.Blessings
	email, clientName, err := b.oauthProvider.GetEmailAndClientName(accessToken, b.accessTokenClients)
	if err != nil {
		return noblessings, "", err
	}
	return b.bless(call.Security(), email, clientName)
}

func (b *oauthBlesser) bless(call security.Call, email, clientName string) (security.Blessings, string, error) {
	var noblessings security.Blessings
	self := call.LocalPrincipal()
	if self == nil {
		return noblessings, "", fmt.Errorf("server error: no authentication happened")
	}
	var caveat security.Caveat
	var err error
	if b.revocationManager != nil {
		caveat, err = b.revocationManager.NewCaveat(self.PublicKey(), b.dischargerLocation)
	} else {
		caveat, err = security.NewExpiryCaveat(time.Now().Add(b.duration))
	}
	if err != nil {
		return noblessings, "", err
	}
	// Append clientName (e.g., "android", "chrome") to the email and then bless under that.
	// Since blessings issued by this process do not have many caveats on them and typically
	// have a large expiry duration, we include the clientName in the extension so that
	// servers can explicitly distinguish these clients while specifying authorization policies
	// (say, via AccessLists).
	extension := strings.Join([]string{email, clientName}, security.ChainSeparator)
	blessing, err := self.Bless(call.RemoteBlessings().PublicKey(), call.LocalBlessings(), extension, caveat)
	if err != nil {
		return noblessings, "", err
	}
	return blessing, extension, nil
}
