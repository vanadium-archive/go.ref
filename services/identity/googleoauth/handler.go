// Package googleoauth implements an http.Handler that uses OAuth 2.0 to
// authenticate with Google and then generates a Veyron identity and
// blesses it with the identity of the HTTP server.
//
// The GoogleIDToken is currently validated by sending an HTTP request to
// googleapis.com.  This adds a round-trip and service may be denied by
// googleapis.com if this handler becomes a breakout success and receives tons
// of traffic.  If either is a concern, the GoogleIDToken can be validated
// without an additional HTTP request.
// See: https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
package googleoauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"code.google.com/p/goauth2/oauth"

	"veyron/services/identity/util"
	"veyron2"
	"veyron2/vlog"
)

type HandlerArgs struct {
	// URL at which the hander is installed.
	// e.g. http://host:port/google/
	// This is where the handler is installed and where redirect requests
	// from Google will come to.
	Addr string
	// client_id and client_secret registered with the Google Developer
	// Console for API access.
	ClientID, ClientSecret string
	// Minimum expiry time (in days) of identities issued by the server
	MinExpiryDays int
	// Runtime from which the identity of the server itself will be extracted,
	// and new identities will be created for blessing.
	Runtime veyron2.Runtime
	// When set, restricts the allowed email addresses to this domain, e.g.
	// google.com.
	RestrictEmailDomain string
}

func (a *HandlerArgs) redirectURL() string { return path.Join(a.Addr, "oauth2callback") }

// URL used to verify google tokens.
// (From https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
// and https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken)
const verifyURL = "https://www.googleapis.com/oauth2/v1/tokeninfo?"

// NewHandler returns an http.Handler that expects to be rooted at args.Addr
// and can be used to use OAuth 2.0 to authenticate with Google, mint a new
// identity and bless it with the Google email address.
func NewHandler(args HandlerArgs) http.Handler {
	config := NewOAuthConfig(args.ClientID, args.ClientSecret, args.redirectURL())
	return &handler{config, args.MinExpiryDays, util.NewCSRFCop(), args.Runtime, args.RestrictEmailDomain}
}

// NewOAuthConfig returns the oauth.Config required for obtaining just the email address from Google using OAuth 2.0.
func NewOAuthConfig(clientID, clientSecret, redirectURL string) *oauth.Config {
	return &oauth.Config{
		ClientId:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scope:        "email",
		AuthURL:      "https://accounts.google.com/o/oauth2/auth",
		TokenURL:     "https://accounts.google.com/o/oauth2/token",
	}
}

type handler struct {
	config        *oauth.Config
	minExpiryDays int
	csrfCop       *util.CSRFCop
	rt            veyron2.Runtime
	domain        string
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch path.Base(r.URL.Path) {
	case "auth":
		h.auth(w, r)
	case "oauth2callback":
		h.callback(w, r)
	default:
		util.HTTPBadRequest(w, r, nil)
	}
}

func (h *handler) auth(w http.ResponseWriter, r *http.Request) {
	csrf, err := h.csrfCop.NewToken(w, r)
	if err != nil {
		vlog.Infof("Failed to create CSRF token[%v] for request %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected automated request: %v", err))
		return
	}
	http.Redirect(w, r, h.config.AuthCodeURL(csrf), http.StatusFound)
}

func (h *handler) callback(w http.ResponseWriter, r *http.Request) {
	if err := h.csrfCop.ValidateToken(r.FormValue("state"), r); err != nil {
		vlog.Infof("Invalid CSRF token: %v in request: %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected request forgery: %v", err))
		return
	}
	email, err := ExchangeAuthCodeForEmail(h.config, r.FormValue("code"))
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	if len(h.domain) > 0 && !strings.HasSuffix(email, "@"+h.domain) {
		util.HTTPServerError(w, fmt.Errorf("email domain in %s is not allowed", email))
		return
	}
	minted, err := h.rt.NewIdentity("unblessed")
	if err != nil {
		util.HTTPServerError(w, fmt.Errorf("Failed to mint new identity: %v", err))
		return
	}
	blessing, err := h.rt.Identity().Bless(minted.PublicID(), email, 24*time.Hour*time.Duration(h.minExpiryDays), nil)
	if err != nil {
		util.HTTPServerError(w, fmt.Errorf("%v.Bless(%q, %q, %d days, nil) failed: %v", h.rt.Identity(), minted, h.minExpiryDays, err))
		return
	}
	blessed, err := minted.Derive(blessing)
	if err != nil {
		util.HTTPServerError(w, fmt.Errorf("%v.Derive(%q) failed: %v", minted, blessed, err))
		return
	}
	vlog.Infof("Created new identity: %v", blessed)
	util.HTTPSend(w, blessed)
}

// ExchangeAuthCodeForEmail exchanges the authorization code (which must
// have been obtained with scope=email) for an OAuth token and then uses Google's
// tokeninfo API to extract the email address from that token.
func ExchangeAuthCodeForEmail(config *oauth.Config, authcode string) (string, error) {
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
	tinfo, err := http.Get(verifyURL + "id_token=" + t.Extra["id_token"])
	if err != nil {
		return "", fmt.Errorf("failed to talk to GoogleIDToken verifier (%q): %v", verifyURL, err)
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
	return gtoken.Email, nil
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
