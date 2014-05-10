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
	"time"

	"code.google.com/p/goauth2/oauth"

	"veyron/services/identity/util"
	"veyron2"
	"veyron2/vlog"
)

type HandlerArgs struct {
	// Whether the HTTP server is using TLS or not.
	UseTLS bool
	// Fully-qualified address (host:port) that the HTTP server is
	// listening on (and where redirect requests from Google will come to).
	Addr string
	// Address prefix (including trailing slash) on which the Google OAuth
	// based Identity generator should run.
	Prefix string
	// client_id and client_secret registered with the Google Developer
	// Console for API access.
	ClientID, ClientSecret string
	// Minimum expiry time (in days) of identities issued by the server
	MinExpiryDays int
	// Runtime from which the identity of the server itself will be extracted,
	// and new identities will be created for blessing.
	Runtime veyron2.Runtime
}

func (a *HandlerArgs) redirectURL() string {
	scheme := "http"
	if a.UseTLS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%soauth2callback", scheme, a.Addr, a.Prefix)
}

// NewHandler returns an http.Handler that expects to be rooted at args.Prefix
// and can be used to use OAuth 2.0 to authenticate with Google, mint a new
// identity and bless it with the Google email address.
func NewHandler(args HandlerArgs) http.Handler {
	config := &oauth.Config{
		ClientId:     args.ClientID,
		ClientSecret: args.ClientSecret,
		RedirectURL:  args.redirectURL(),
		Scope:        "email",
		AuthURL:      "https://accounts.google.com/o/oauth2/auth",
		TokenURL:     "https://accounts.google.com/o/oauth2/token",
	}
	verifyURL := "https://www.googleapis.com/oauth2/v1/tokeninfo?id_token="
	return &handler{config, args.Addr, args.MinExpiryDays, util.NewCSRFCop(), args.Runtime, verifyURL}
}

type handler struct {
	config        *oauth.Config
	issuer        string
	minExpiryDays int
	csrfCop       *util.CSRFCop
	rt            veyron2.Runtime
	verifyURL     string
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
	transport := &oauth.Transport{Config: h.config}
	t, err := transport.Exchange(r.FormValue("code"))
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("Failed to fetch GoogleIDToken: %v", err))
		return
	}
	// Ideally, would validate the token ourselves without an HTTP roundtrip.
	// However, for now, as per:
	// https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
	// pay an HTTP round-trip to have Google do this.
	if t.Extra == nil {
		util.HTTPServerError(w, fmt.Errorf("No GoogleIDToken found in exchange"))
		return
	}
	tinfo, err := http.Get(h.verifyURL + t.Extra["id_token"])
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	if tinfo.StatusCode != http.StatusOK {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to decode GoogleIDToken: %q", tinfo.Status))
		return
	}
	var gtoken token
	if err = json.NewDecoder(tinfo.Body).Decode(&gtoken); err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("invalid response from Google's tokeninfo API: %v", err))
		return
	}
	if !gtoken.VerifiedEmail {
		util.HTTPBadRequest(w, r, fmt.Errorf("email not verified: %#v", gtoken))
		return
	}
	if gtoken.Issuer != "accounts.google.com" {
		util.HTTPBadRequest(w, r, fmt.Errorf("invalid issuer: %v", gtoken.Issuer))
		return
	}
	if gtoken.Audience != h.config.ClientId {
		util.HTTPBadRequest(w, r, fmt.Errorf("unexpected audience(%v) in GoogleIDToken", gtoken.Audience))
		return
	}
	minted, err := h.rt.NewIdentity("unblessed")
	if err != nil {
		util.HTTPServerError(w, fmt.Errorf("Failed to mint new identity: %v", err))
		return
	}
	blessing, err := h.rt.Identity().Bless(minted.PublicID(), gtoken.Email, 24*time.Hour*time.Duration(h.minExpiryDays), nil)
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
