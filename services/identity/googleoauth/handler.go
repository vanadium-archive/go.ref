// Package googleoauth implements an http.Handler that uses OAuth 2.0 to
// authenticate with Google and then renders a page that displays all the
// blessings that were provided for that Google user.
//
// The GoogleIDToken is currently validated by sending an HTTP request to
// googleapis.com.  This adds a round-trip and service may be denied by
// googleapis.com if this handler becomes a breakout success and receives tons
// of traffic.  If either is a concern, the GoogleIDToken can be validated
// without an additional HTTP request.
// See: https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
package googleoauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"
	"time"

	"code.google.com/p/goauth2/oauth"

	"veyron/services/identity/auditor"
	"veyron/services/identity/revocation"
	"veyron/services/identity/util"
	"veyron2/security"
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
	// Prefix for the audit log from which data will be sourced.
	// (auditor.ReadAuditLog).
	Auditor string
	// The RevocationManager is used to revoke blessings granted with a revocation caveat.
	RevocationManager *revocation.RevocationManager
}

func (a *HandlerArgs) redirectURL() string {
	ret := a.Addr
	if !strings.HasSuffix(ret, "/") {
		ret += "/"
	}
	ret += "oauth2callback"
	return ret
}

// URL used to verify google tokens.
// (From https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
// and https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken)
const verifyURL = "https://www.googleapis.com/oauth2/v1/tokeninfo?"

// NewHandler returns an http.Handler that expects to be rooted at args.Addr
// and can be used to use OAuth 2.0 to authenticate with Google, mint a new
// identity and bless it with the Google email address.
func NewHandler(args HandlerArgs) http.Handler {
	config := NewOAuthConfig(args.ClientID, args.ClientSecret, args.redirectURL())
	tokenMap := make(map[tokenCaveatIDKey]bool)
	return &handler{config, util.NewCSRFCop(), args.Auditor, args.RevocationManager, tokenMap}
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

type tokenCaveatIDKey struct {
	token, caveatID string
}

type handler struct {
	config                   *oauth.Config
	csrfCop                  *util.CSRFCop
	auditor                  string
	revocationManager        *revocation.RevocationManager
	tokenRevocationCaveatMap map[tokenCaveatIDKey]bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch path.Base(r.URL.Path) {
	case "auth":
		h.auth(w, r)
	case "oauth2callback":
		h.callback(w, r)
	case "revoke":
		h.revoke(w, r)
	default:
		util.HTTPBadRequest(w, r, nil)
	}
}

func (h *handler) auth(w http.ResponseWriter, r *http.Request) {
	csrf, err := h.csrfCop.NewToken(w, r, "VeyronHTTPIdentityClientID")
	if err != nil {
		vlog.Infof("Failed to create CSRF token[%v] for request %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected automated request: %v", err))
		return
	}
	http.Redirect(w, r, h.config.AuthCodeURL(csrf), http.StatusFound)
}

func (h *handler) callback(w http.ResponseWriter, r *http.Request) {
	if err := h.csrfCop.ValidateToken(r.FormValue("state"), r, "VeyronHTTPIdentityClientID"); err != nil {
		vlog.Infof("Invalid CSRF token: %v in request: %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected request forgery: %v", err))
		return
	}
	email, err := ExchangeAuthCodeForEmail(h.config, r.FormValue("code"))
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	// Create a new token to protect ensure that the revocation calls are protected.
	csrf, err := h.csrfCop.NewToken(w, r, "VeyronHTTPIdentityRevocationID")
	if err != nil {
		vlog.Infof("Failed to create CSRF token[%v] for request %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected automated request: %v", err))
		return
	}
	type tmplentry struct {
		Blessee            security.PublicID
		Start, End         time.Time
		Blessed            security.PublicID
		RevocationCaveatID string
	}
	tmplargs := struct {
		Log              chan tmplentry
		Email, CSRFToken string
	}{
		Log:       make(chan tmplentry),
		Email:     email,
		CSRFToken: csrf,
	}
	if entrych, err := auditor.ReadAuditLog(h.auditor, email); err != nil {
		vlog.Errorf("Unable to read audit log: %v", err)
		util.HTTPServerError(w, fmt.Errorf("unable to read audit log"))
		return
	} else {
		go func(ch chan tmplentry) {
			defer close(ch)
			for entry := range entrych {
				if entry.Method != "Bless" || len(entry.Arguments) < 2 {
					continue
				}
				var tmplentry tmplentry
				var blessEntry revocation.BlessingAuditEntry
				blessEntry, err = revocation.ReadBlessAuditEntry(entry)
				tmplentry.Blessee = blessEntry.Blessee
				tmplentry.Blessed = blessEntry.Blessed
				tmplentry.Start = blessEntry.Start
				tmplentry.End = blessEntry.End
				if err != nil {
					vlog.Errorf("Unable to read bless audit entry: %v", err)
					continue
				}
				if blessEntry.RevocationCaveat != nil {
					tmplentry.RevocationCaveatID = base64.URLEncoding.EncodeToString([]byte(blessEntry.RevocationCaveat.ID()))
					// TODO(suharshs): Add a timeout that removes old entries to reduce storage space.
					// TODO(suharshs): Make this map from CSRFToken to Email address, have
					// revocation manager have map from caveatID to Email address in DirectoryStore.
					h.tokenRevocationCaveatMap[tokenCaveatIDKey{csrf, tmplentry.RevocationCaveatID}] = true
				}
				// TODO(suharshs): Make the UI depend on where the caveatID exists and if it hasn't been revoked.
				// Use the revocation manager IsRevoked function.
				ch <- tmplentry
			}
		}(tmplargs.Log)
	}
	w.Header().Set("Context-Type", "text/html")
	if err := tmpl.Execute(w, tmplargs); err != nil {
		vlog.Errorf("Unable to execute audit page template: %v", err)
		util.HTTPServerError(w, err)
	}
}

func (h *handler) revoke(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	const (
		success = `{"success": "true"}`
		failure = `{"success": "false"}`
	)
	if h.revocationManager == nil {
		vlog.Infof("no provided revocation manager")
		w.Write([]byte(failure))
		return
	}

	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		vlog.Infof("Failed to parse request: %s", err)
		w.Write([]byte(failure))
		return
	}
	var requestParams struct {
		CaveatID, CSRFToken string
	}
	if err := json.Unmarshal(content, &requestParams); err != nil {
		vlog.Infof("json.Unmarshal failed : %s", err)
		w.Write([]byte(failure))
		return
	}

	if err := h.validateTokenCaveatID(requestParams.CSRFToken, requestParams.CaveatID, r); err != nil {
		vlog.Infof("failed to validate token for caveat: %s", err)
		w.Write([]byte(failure))
		return
	}

	decodedCaveatID, err := base64.URLEncoding.DecodeString(requestParams.CaveatID)
	if err != nil {
		vlog.Infof("base64 decoding failed: %s", err)
		w.Write([]byte(failure))
		return
	}

	caveatID := security.ThirdPartyCaveatID(string(decodedCaveatID))

	if err := h.revocationManager.Revoke(caveatID); err != nil {
		vlog.Infof("Revocation failed: %s", err)
		w.Write([]byte(failure))
		return
	}

	w.Write([]byte(success))
	return
}

func (h *handler) validateTokenCaveatID(CSRFToken, revocationCaveatID string, r *http.Request) error {
	if err := h.csrfCop.ValidateToken(CSRFToken, r, "VeyronHTTPIdentityRevocationID"); err != nil {
		return fmt.Errorf("invalid CSRF token: %v in request: %#v", err, r)
	}
	if _, exists := h.tokenRevocationCaveatMap[tokenCaveatIDKey{CSRFToken, revocationCaveatID}]; exists {
		return nil
	}
	return fmt.Errorf("this token has no matching entry for the provided caveat ID")
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
