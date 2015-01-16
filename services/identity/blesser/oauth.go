package blesser

import (
	"fmt"
	"strings"
	"time"

	"v.io/core/veyron/services/identity"
	"v.io/core/veyron/services/identity/oauth"
	"v.io/core/veyron/services/identity/revocation"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
)

type oauthBlesser struct {
	oauthProvider      oauth.OAuthProvider
	authcodeClient     struct{ ID, Secret string }
	accessTokenClients []oauth.AccessTokenClient
	duration           time.Duration
	domain             string
	dischargerLocation string
	revocationManager  revocation.RevocationManager
}

// OAuthBlesserParams represents all the parameters provided to NewOAuthBlesserServer
type OAuthBlesserParams struct {
	// The OAuth provider that must have issued the access tokens accepted by ths service.
	OAuthProvider oauth.OAuthProvider
	// The OAuth client IDs and names for the clients of the BlessUsingAccessToken RPCs.
	AccessTokenClients []oauth.AccessTokenClient
	// If non-empty, only email addresses from this domain will be blessed.
	DomainRestriction string
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
// expires after the duration specified by the params. If domain is non-empty, then
// blessings are generated only for email addresses from that domain.
func NewOAuthBlesserServer(p OAuthBlesserParams) identity.OAuthBlesserServerStub {
	return identity.OAuthBlesserServer(&oauthBlesser{
		oauthProvider:      p.OAuthProvider,
		duration:           p.BlessingDuration,
		domain:             p.DomainRestriction,
		dischargerLocation: p.DischargerLocation,
		revocationManager:  p.RevocationManager,
		accessTokenClients: p.AccessTokenClients,
	})
}

func (b *oauthBlesser) BlessUsingAccessToken(ctx ipc.ServerContext, accessToken string) (security.WireBlessings, string, error) {
	var noblessings security.WireBlessings
	email, clientName, err := b.oauthProvider.GetEmailAndClientName(accessToken, b.accessTokenClients)
	if err != nil {
		return noblessings, "", err
	}
	return b.bless(ctx, email, clientName)
}

func (b *oauthBlesser) bless(ctx ipc.ServerContext, email, clientName string) (security.WireBlessings, string, error) {
	var noblessings security.WireBlessings
	if len(b.domain) > 0 && !strings.HasSuffix(email, "@"+b.domain) {
		return noblessings, "", fmt.Errorf("domain restrictions preclude blessings for %q", email)
	}
	// Append clientName (e.g., "android", "chrome") to the email and then bless under that.
	// Since blessings issued by this process do not have many caveats on them and typically
	// have a large expiry duration, we include the clientName in the extension so that
	// servers can explicitly distinguish these clients while specifying authorization policies
	// (say, via ACLs).
	extension := email + security.ChainSeparator + clientName
	self := ctx.LocalPrincipal()
	if self == nil {
		return noblessings, "", fmt.Errorf("server error: no authentication happened")
	}
	var caveat security.Caveat
	var err error
	if b.revocationManager != nil {
		caveat, err = b.revocationManager.NewCaveat(self.PublicKey(), b.dischargerLocation)
	} else {
		caveat, err = security.ExpiryCaveat(time.Now().Add(b.duration))
	}
	if err != nil {
		return noblessings, "", err
	}
	blessing, err := self.Bless(ctx.RemoteBlessings().PublicKey(), ctx.LocalBlessings(), extension, caveat)
	if err != nil {
		return noblessings, "", err
	}
	return security.MarshalBlessings(blessing), extension, nil
}
