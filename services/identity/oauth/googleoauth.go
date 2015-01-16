package oauth

import (
	"code.google.com/p/goauth2/oauth"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"v.io/core/veyron2/vlog"
)

// googleOAuth implements the OAuthProvider interface with google oauth 2.0.
type googleOAuth struct {
	// client_id and client_secret registered with the Google Developer
	// Console for API access.
	clientID, clientSecret   string
	scope, authURL, tokenURL string
	domain                   string
	// URL used to verify google tokens.
	// (From https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
	// and https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken)
	verifyURL string
}

func NewGoogleOAuth(configFile, domainRestriction string) (OAuthProvider, error) {
	clientID, clientSecret, err := getOAuthClientIDAndSecret(configFile)
	if err != nil {
		return nil, err
	}
	return &googleOAuth{
		clientID:     clientID,
		clientSecret: clientSecret,
		scope:        "email",
		authURL:      "https://accounts.google.com/o/oauth2/auth",
		tokenURL:     "https://accounts.google.com/o/oauth2/token",
		verifyURL:    "https://www.googleapis.com/oauth2/v1/tokeninfo?",
		domain:       domainRestriction,
	}, nil
}

func (g *googleOAuth) AuthURL(redirectUrl, state string) string {
	return g.oauthConfig(redirectUrl).AuthCodeURL(state)
}

// ExchangeAuthCodeForEmail exchanges the authorization code (which must
// have been obtained with scope=email) for an OAuth token and then uses Google's
// tokeninfo API to extract the email address from that token.
func (g *googleOAuth) ExchangeAuthCodeForEmail(authcode string, url string) (string, error) {
	config := g.oauthConfig(url)
	t, err := (&oauth.Transport{Config: config}).Exchange(authcode)
	if err != nil {
		return "", fmt.Errorf("failed to exchange authorization code for token: %v", err)
	}
	// Ideally, would validate the token ourselves without an HTTP roundtrip.
	// However, for now, as per:
	// https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
	// pay an HTTP round-trip to have Google do this.
	if t.Extra == nil || len(t.Extra["id_token"]) == 0 {
		return "", fmt.Errorf("no GoogleIDToken found in OAuth token")
	}
	// The GoogleIDToken is currently validated by sending an HTTP request to
	// googleapis.com.  This adds a round-trip and service may be denied by
	// googleapis.com if this handler becomes a breakout success and receives tons
	// of traffic.  If either is a concern, the GoogleIDToken can be validated
	// without an additional HTTP request.
	// See: https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
	tinfo, err := http.Get(g.verifyURL + "id_token=" + t.Extra["id_token"])
	if err != nil {
		return "", fmt.Errorf("failed to talk to GoogleIDToken verifier (%q): %v", g.verifyURL, err)
	}
	if tinfo.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to verify GoogleIDToken: %s", tinfo.Status)
	}
	var gtoken token
	if err := json.NewDecoder(tinfo.Body).Decode(&gtoken); err != nil {
		return "", fmt.Errorf("invalid JSON response from Google's tokeninfo API: %v", err)
	}
	if !gtoken.VerifiedEmail {
		return "", fmt.Errorf("email not verified: %#v", gtoken)
	}
	if gtoken.Issuer != "accounts.google.com" {
		return "", fmt.Errorf("invalid issuer: %v", gtoken.Issuer)
	}
	if gtoken.Audience != config.ClientId {
		return "", fmt.Errorf("unexpected audience(%v) in GoogleIDToken", gtoken.Audience)
	}
	if len(g.domain) > 0 && !strings.HasSuffix(gtoken.Email, "@"+g.domain) {
		return "", fmt.Errorf("domain restrictions preclude %q from using this service", gtoken.Email)
	}

	return gtoken.Email, nil
}

// GetEmailAndClientName uses Google's tokeninfo API to verify that the token has been issued
// for one of the provided 'accessTokenClients' and if so returns the email and client name
// from the tokeninfo obtained.
func (g *googleOAuth) GetEmailAndClientName(accessToken string, accessTokenClients []AccessTokenClient) (string, string, error) {
	if len(accessTokenClients) == 0 {
		return "", "", fmt.Errorf("no expected AccessTokenClients specified")
	}
	// As per https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken
	// we obtain the 'info' for the token via an HTTP roundtrip to Google.
	tokeninfo, err := http.Get(g.verifyURL + "access_token=" + accessToken)
	if err != nil {
		return "", "", fmt.Errorf("unable to use token: %v", err)
	}
	if tokeninfo.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unable to verify access token, OAuth2 TokenInfo endpoint responded with StatusCode: %v", tokeninfo.StatusCode)
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
		return "", "", fmt.Errorf("invalid JSON response from Google's tokeninfo API: %v", err)
	}
	var client AccessTokenClient
	audienceMatch := false
	for _, c := range accessTokenClients {
		if token.Audience == c.ClientID {
			client = c
			audienceMatch = true
			break
		}
	}
	if !audienceMatch {
		vlog.Infof("Got access token [%+v], wanted one of client ids %v", token, accessTokenClients)
		return "", "", fmt.Errorf("token not meant for this purpose, confused deputy? https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken")
	}
	if !token.VerifiedEmail {
		return "", "", fmt.Errorf("email not verified")
	}
	return token.Email, client.Name, nil
}

func (g *googleOAuth) oauthConfig(redirectUrl string) *oauth.Config {
	return &oauth.Config{
		ClientId:     g.clientID,
		ClientSecret: g.clientSecret,
		RedirectURL:  redirectUrl,
		Scope:        g.scope,
		AuthURL:      g.authURL,
		TokenURL:     g.tokenURL,
	}
}

func getOAuthClientIDAndSecret(configFile string) (clientID, clientSecret string, err error) {
	f, err := os.Open(configFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to open %q: %v", configFile, err)
	}
	defer f.Close()
	clientID, clientSecret, err = ClientIDAndSecretFromJSON(f)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode JSON in %q: %v", configFile, err)
	}
	return clientID, clientSecret, nil
}

// IDToken JSON message returned by Google's verification endpoint.
//
// This differs from the description in:
// https://developers.google.com/accounts/docs/OAuth2Login#obtainuserinfo
// because the Google tokeninfo endpoint
// (https://www.googleapis.com/oauth2/v1/tokeninfo?id_token=XYZ123)
// mentioned in:
// https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
// seems to return the following JSON message.
type token struct {
	Issuer        string `json:"issuer"`
	IssuedTo      string `json:"issued_to"`
	Audience      string `json:"audience"`
	UserID        string `json:"user_id"`
	ExpiresIn     int64  `json:"expires_in"`
	IssuedAt      int64  `json:"issued_at"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
}
