package blesser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"v.io/core/veyron/services/identity"
	"v.io/core/veyron/services/identity/revocation"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"
)

type googleOAuth struct {
	authcodeClient     struct{ ID, Secret string }
	accessTokenClients []AccessTokenClient
	duration           time.Duration
	domain             string
	dischargerLocation string
	revocationManager  revocation.RevocationManager
}

// AccessTokenClient represents a client of the BlessUsingAccessToken RPCs.
type AccessTokenClient struct {
	// Descriptive name of the client.
	Name string
	// OAuth Client ID.
	ClientID string
}

// GoogleParams represents all the parameters provided to NewGoogleOAuthBlesserServer
type GoogleParams struct {
	// The OAuth client IDs and names for the clients of the BlessUsingAccessToken RPCs.
	AccessTokenClients []AccessTokenClient
	// If non-empty, only email addresses from this domain will be blessed.
	DomainRestriction string
	// The object name of the discharger service. If this is empty then revocation caveats will not be granted.
	DischargerLocation string
	// The revocation manager that generates caveats and manages revocation.
	RevocationManager revocation.RevocationManager
	// The duration for which blessings will be valid. (Used iff RevocationManager is nil).
	BlessingDuration time.Duration
}

// NewGoogleOAuthBlesserServer provides an identity.OAuthBlesserService that uses authorization
// codes to obtain the username of a client and provide blessings with that name.
//
// For more details, see documentation on Google OAuth 2.0 flows:
// https://developers.google.com/accounts/docs/OAuth2
//
// Blessings generated by this server expire after duration. If domain is non-empty, then blessings
// are generated only for email addresses from that domain.
func NewGoogleOAuthBlesserServer(p GoogleParams) interface{} {
	return identity.OAuthBlesserServer(&googleOAuth{
		duration:           p.BlessingDuration,
		domain:             p.DomainRestriction,
		dischargerLocation: p.DischargerLocation,
		revocationManager:  p.RevocationManager,
		accessTokenClients: p.AccessTokenClients,
	})
}

func (b *googleOAuth) BlessUsingAccessToken(ctx ipc.ServerContext, accesstoken string) (security.WireBlessings, string, error) {
	var noblessings security.WireBlessings
	if len(b.accessTokenClients) == 0 {
		return noblessings, "", fmt.Errorf("server not configured for blessing based on access tokens")
	}
	// URL from: https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken
	tokeninfo, err := http.Get("https://www.googleapis.com/oauth2/v1/tokeninfo?access_token=" + accesstoken)
	if err != nil {
		return noblessings, "", fmt.Errorf("unable to use token: %v", err)
	}
	if tokeninfo.StatusCode != http.StatusOK {
		return noblessings, "", fmt.Errorf("unable to verify access token: %v", tokeninfo.StatusCode)
	}
	// tokeninfo contains a JSON-encoded struct
	var token struct {
		IssuedTo      string `json:"issued_to"`
		Audience      string `json:"audience"`
		UserID        string `json:"user_id"`
		Scope         string `json:"scope"`
		ExpiresIn     int64  `json:"expires_in"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		AccessType    string `json:"access_type"`
	}
	if err := json.NewDecoder(tokeninfo.Body).Decode(&token); err != nil {
		return noblessings, "", fmt.Errorf("invalid JSON response from Google's tokeninfo API: %v", err)
	}
	var client AccessTokenClient
	audienceMatch := false
	for _, c := range b.accessTokenClients {
		if token.Audience == c.ClientID {
			client = c
			audienceMatch = true
			break
		}
	}
	if !audienceMatch {
		vlog.Infof("Got access token [%+v], wanted one of client ids %v", token, b.accessTokenClients)
		return noblessings, "", fmt.Errorf("token not meant for this purpose, confused deputy? https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken")
	}
	if !token.VerifiedEmail {
		return noblessings, "", fmt.Errorf("email not verified")
	}
	// Append client.Name to the blessing (e.g., "android", "chrome"). Since blessings issued by
	// this process do not have many caveats on them and typically have a large expiry duration,
	// we append this suffix so that servers can explicitly distinguish these clients while
	// specifying authorization policies (say, via ACLs).
	return b.bless(ctx, token.Email+security.ChainSeparator+client.Name)
}

func (b *googleOAuth) bless(ctx ipc.ServerContext, extension string) (security.WireBlessings, string, error) {
	var noblessings security.WireBlessings
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
