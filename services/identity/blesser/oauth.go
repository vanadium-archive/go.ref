package blesser

import (
	"time"

	"veyron/services/identity"
	"veyron/services/identity/googleoauth"
	"veyron2"
	"veyron2/ipc"
	"veyron2/vdl/vdlutil"
)

// Expiry time of blessings issued by the Google OAuth Blesser Server.
// TODO(ashankar): This is ridiculously large! Add third-party revocation
// caveat instead?
const BlessingExpiry = 365 * 24 * time.Hour

type googleOAuth struct {
	rt                     veyron2.Runtime
	clientID, clientSecret string
}

// NewGoogleOAuthBlesserServer provides an identity.OAuthBlesserService that uses authorization
// codes to obtain the username of a client and provide blessings with that name.
//
// For more details, see documentation on Google OAuth 2.0 flows:
// https://developers.google.com/accounts/docs/OAuth2
func NewGoogleOAuthBlesserServer(rt veyron2.Runtime, clientID, clientSecret string) interface{} {
	return identity.NewServerOAuthBlesser(&googleOAuth{rt, clientID, clientSecret})
}

func (b *googleOAuth) Bless(ctx ipc.ServerContext, authcode, redirectURL string) (vdlutil.Any, error) {
	config := googleoauth.NewOAuthConfig(b.clientID, b.clientSecret, redirectURL)
	name, err := googleoauth.ExchangeAuthCodeForEmail(config, authcode)
	if err != nil {
		return nil, err
	}
	self := b.rt.Identity()
	// Use the blessing that was used to authenticate with the client to bless it.
	if self, err = self.Derive(ctx.LocalID()); err != nil {
		return nil, err
	}
	// TODO(ashankar,ataly): Use the same set of caveats as is used by the HTTP handler.
	// For example, a third-party revocation caveat?
	// TODO(ashankar,rthellend): Also want the domain restriction here?
	return self.Bless(ctx.RemoteID(), name, BlessingExpiry, nil)
}
