// Package googleoauth implements an http.Handler that has two main purposes
// listed below:

// (1) Uses OAuth 2.0 to authenticate with Google and then renders a page that
//     displays all the blessings that were provided for that Google user.
//     The client calls the /listblessings route which redirects to listblessingscallback which
//     renders the list.
// (2) Performs the oauth flow for seeking a blessing using the identity tool
//     located at veyron/tools/identity.
//     The seek blessing flow works as follows:
//     (a) Client (identity tool) hits the /seekblessings route.
//     (b) /seekblessings performs google oauth with a redirect to /seekblessingscallback.
//     (c) Client specifies desired caveats in the form that /seekblessingscallback displays.
//     (d) Submission of the form sends caveat information to /sendmacaroon.
//     (e) /sendmacaroon sends a macaroon with blessing information to client
//         (via a redirect to an HTTP server run by the tool).
//     (f) Client invokes bless rpc with macaroon.
//
// The GoogleIDToken is currently validated by sending an HTTP request to
// googleapis.com.  This adds a round-trip and service may be denied by
// googleapis.com if this handler becomes a breakout success and receives tons
// of traffic.  If either is a concern, the GoogleIDToken can be validated
// without an additional HTTP request.
// See: https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
package googleoauth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"code.google.com/p/goauth2/oauth"

	"veyron.io/veyron/veyron/services/identity/auditor"
	"veyron.io/veyron/veyron/services/identity/blesser"
	"veyron.io/veyron/veyron/services/identity/revocation"
	"veyron.io/veyron/veyron/services/identity/util"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
)

const (
	clientIDCookie   = "VeyronHTTPIdentityClientID"
	revocationCookie = "VeyronHTTPIdentityRevocationID"

	ListBlessingsRoute         = "listblessings"
	listBlessingsCallbackRoute = "listblessingscallback"
	revokeRoute                = "revoke"
	SeekBlessingsRoute         = "seekblessings"
	addCaveatsRoute            = "addcaveats"
	sendMacaroonRoute          = "sendmacaroon"
)

type HandlerArgs struct {
	// The Veyron runtime to use
	R veyron2.Runtime
	// The Key that is used for creating and verifying macaroons.
	// This needs to be common between the handler and the MacaroonBlesser service.
	MacaroonKey []byte
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
	// If this is empty then revocation caveats will not be added to blessings, and instead
	// Expiry Caveats of duration BlessingDuration will be added.
	RevocationManager *revocation.RevocationManager
	// The object name of the discharger service.
	DischargerLocation string
	// MacaroonBlessingService is the object name to which macaroons create by this HTTP
	// handler can be exchanged for a blessing.
	MacaroonBlessingService string
	// If non-empty, only email addressses from this domain will be blessed.
	DomainRestriction string
	// BlessingDuration is the duration that blessings granted will be valid for
	// if RevocationManager is nil.
	BlessingDuration time.Duration
}

func (a *HandlerArgs) oauthConfig(redirectSuffix string) *oauth.Config {
	return &oauth.Config{
		ClientId:     a.ClientID,
		ClientSecret: a.ClientSecret,
		RedirectURL:  redirectURL(a.Addr, redirectSuffix),
		Scope:        "email",
		AuthURL:      "https://accounts.google.com/o/oauth2/auth",
		TokenURL:     "https://accounts.google.com/o/oauth2/token",
	}
}

func redirectURL(baseURL, suffix string) string {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return baseURL + suffix
}

// URL used to verify google tokens.
// (From https://developers.google.com/accounts/docs/OAuth2Login#validatinganidtoken
// and https://developers.google.com/accounts/docs/OAuth2UserAgent#validatetoken)
const verifyURL = "https://www.googleapis.com/oauth2/v1/tokeninfo?"

// NewHandler returns an http.Handler that expects to be rooted at args.Addr
// and can be used to use OAuth 2.0 to authenticate with Google, mint a new
// identity and bless it with the Google email address.
func NewHandler(args HandlerArgs) (http.Handler, error) {
	csrfCop, err := util.NewCSRFCop()
	if err != nil {
		return nil, fmt.Errorf("NewHandler failed to create csrfCop: %v", err)
	}
	return &handler{
		args:    args,
		csrfCop: csrfCop,
	}, nil
}

type handler struct {
	args    HandlerArgs
	csrfCop *util.CSRFCop
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch path.Base(r.URL.Path) {
	case ListBlessingsRoute:
		h.listBlessings(w, r)
	case listBlessingsCallbackRoute:
		h.listBlessingsCallback(w, r)
	case revokeRoute:
		h.revoke(w, r)
	case SeekBlessingsRoute:
		h.seekBlessings(w, r)
	case addCaveatsRoute:
		h.addCaveats(w, r)
	case sendMacaroonRoute:
		h.sendMacaroon(w, r)
	default:
		util.HTTPBadRequest(w, r, nil)
	}
}

func (h *handler) listBlessings(w http.ResponseWriter, r *http.Request) {
	csrf, err := h.csrfCop.NewToken(w, r, clientIDCookie, nil)
	if err != nil {
		vlog.Infof("Failed to create CSRF token[%v] for request %#v", err, r)
		util.HTTPServerError(w, fmt.Errorf("failed to create new token: %v", err))
		return
	}
	http.Redirect(w, r, h.args.oauthConfig(listBlessingsCallbackRoute).AuthCodeURL(csrf), http.StatusFound)
}

func (h *handler) listBlessingsCallback(w http.ResponseWriter, r *http.Request) {
	if err := h.csrfCop.ValidateToken(r.FormValue("state"), r, clientIDCookie, nil); err != nil {
		vlog.Infof("Invalid CSRF token: %v in request: %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected request forgery: %v", err))
		return
	}
	email, err := exchangeAuthCodeForEmail(h.args.oauthConfig(listBlessingsCallbackRoute), r.FormValue("code"))
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}

	type tmplentry struct {
		Blessee        security.PublicID
		Start, End     time.Time
		Blessed        security.PublicID
		RevocationTime time.Time
		Token          string
	}
	tmplargs := struct {
		Log                chan tmplentry
		Email, RevokeRoute string
	}{
		Log:         make(chan tmplentry),
		Email:       email,
		RevokeRoute: revokeRoute,
	}
	if entrych, err := auditor.ReadAuditLog(h.args.Auditor, email); err != nil {
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
					if revocationTime := h.args.RevocationManager.GetRevocationTime(blessEntry.RevocationCaveat.ID()); revocationTime != nil {
						tmplentry.RevocationTime = *revocationTime
					} else {
						caveatID := base64.URLEncoding.EncodeToString([]byte(blessEntry.RevocationCaveat.ID()))
						if tmplentry.Token, err = h.csrfCop.NewToken(w, r, revocationCookie, caveatID); err != nil {
							vlog.Infof("Failed to create CSRF token[%v] for request %#v", err, r)
							util.HTTPServerError(w, fmt.Errorf("failed to create new token: %v", err))
							return
						}
					}
				}
				ch <- tmplentry
			}
		}(tmplargs.Log)
	}
	w.Header().Set("Context-Type", "text/html")
	// This MaybeSetCookie call is needed to ensure that a cookie is created. Since the
	// header cannot be changed once the body is written to, this needs to be called first.
	if _, err = h.csrfCop.MaybeSetCookie(w, r, revocationCookie); err != nil {
		vlog.Infof("Failed to set CSRF cookie[%v] for request %#v", err, r)
		util.HTTPServerError(w, err)
		return
	}
	if err := tmplViewBlessings.Execute(w, tmplargs); err != nil {
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
	if h.args.RevocationManager == nil {
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
		Token string
	}
	if err := json.Unmarshal(content, &requestParams); err != nil {
		vlog.Infof("json.Unmarshal failed : %s", err)
		w.Write([]byte(failure))
		return
	}

	var caveatID string
	if caveatID, err = h.validateRevocationToken(requestParams.Token, r); err != nil {
		vlog.Infof("failed to validate token for caveat: %s", err)
		w.Write([]byte(failure))
		return
	}
	if err := h.args.RevocationManager.Revoke(caveatID); err != nil {
		vlog.Infof("Revocation failed: %s", err)
		w.Write([]byte(failure))
		return
	}

	w.Write([]byte(success))
	return
}

func (h *handler) validateRevocationToken(Token string, r *http.Request) (string, error) {
	var encCaveatID string
	if err := h.csrfCop.ValidateToken(Token, r, revocationCookie, &encCaveatID); err != nil {
		return "", fmt.Errorf("invalid CSRF token: %v in request: %#v", err, r)
	}
	caveatID, err := base64.URLEncoding.DecodeString(encCaveatID)
	if err != nil {
		return "", fmt.Errorf("decode caveatID failed: %v", err)
	}
	return string(caveatID), nil
}

type seekBlessingsMacaroon struct {
	RedirectURL, State string
}

func validLoopbackURL(u string) (*url.URL, error) {
	netURL, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %v", err)
	}
	// Remove the port from the netURL.Host.
	host, _, err := net.SplitHostPort(netURL.Host)
	// Check if its localhost or loopback ip
	if host == "localhost" {
		return netURL, nil
	}
	urlIP := net.ParseIP(host)
	if urlIP.IsLoopback() {
		return netURL, nil
	}
	return nil, fmt.Errorf("invalid loopback url")
}

func (h *handler) seekBlessings(w http.ResponseWriter, r *http.Request) {
	redirect := r.FormValue("redirect_url")
	if _, err := validLoopbackURL(redirect); err != nil {
		vlog.Infof("seekBlessings failed: invalid redirect_url: %v", err)
		util.HTTPBadRequest(w, r, fmt.Errorf("invalid redirect_url: %v", err))
		return
	}
	outputMacaroon, err := h.csrfCop.NewToken(w, r, clientIDCookie, seekBlessingsMacaroon{
		RedirectURL: redirect,
		State:       r.FormValue("state"),
	})
	if err != nil {
		vlog.Infof("Failed to create CSRF token[%v] for request %#v", err, r)
		util.HTTPServerError(w, fmt.Errorf("failed to create new token: %v", err))
		return
	}
	http.Redirect(w, r, h.args.oauthConfig(addCaveatsRoute).AuthCodeURL(outputMacaroon), http.StatusFound)
}

type addCaveatsMacaroon struct {
	ToolRedirectURL, ToolState, Email string
}

func (h *handler) addCaveats(w http.ResponseWriter, r *http.Request) {
	var inputMacaroon seekBlessingsMacaroon
	if err := h.csrfCop.ValidateToken(r.FormValue("state"), r, clientIDCookie, &inputMacaroon); err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected request forgery: %v", err))
		return
	}
	email, err := exchangeAuthCodeForEmail(h.args.oauthConfig(addCaveatsRoute), r.FormValue("code"))
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	if len(h.args.DomainRestriction) > 0 && !strings.HasSuffix(email, "@"+h.args.DomainRestriction) {
		util.HTTPBadRequest(w, r, fmt.Errorf("blessings for name %q are not allowed due to domain restriction", email))
		return
	}
	outputMacaroon, err := h.csrfCop.NewToken(w, r, clientIDCookie, addCaveatsMacaroon{
		ToolRedirectURL: inputMacaroon.RedirectURL,
		ToolState:       inputMacaroon.State,
		Email:           email,
	})
	if err != nil {
		vlog.Infof("Failed to create caveatForm token[%v] for request %#v", err, r)
		util.HTTPServerError(w, fmt.Errorf("failed to create new token: %v", err))
		return
	}
	tmplargs := struct {
		Extension               string
		CaveatMap               map[string]caveatInfo
		Macaroon, MacaroonRoute string
	}{email, caveatMap, outputMacaroon, sendMacaroonRoute}
	w.Header().Set("Context-Type", "text/html")
	if err := tmplSelectCaveats.Execute(w, tmplargs); err != nil {
		vlog.Errorf("Unable to execute bless page template: %v", err)
		util.HTTPServerError(w, err)
	}
}

func (h *handler) sendMacaroon(w http.ResponseWriter, r *http.Request) {
	var inputMacaroon addCaveatsMacaroon
	if err := h.csrfCop.ValidateToken(r.FormValue("macaroon"), r, clientIDCookie, &inputMacaroon); err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected request forgery: %v", err))
		return
	}
	caveats, err := h.caveats(r)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to extract caveats: ", err))
		return
	}
	buf := &bytes.Buffer{}
	m := blesser.BlessingMacaroon{
		Creation: time.Now(),
		Caveats:  caveats,
		Name:     inputMacaroon.Email,
	}
	if err := vom.NewEncoder(buf).Encode(m); err != nil {
		util.HTTPServerError(w, fmt.Errorf("failed to encode BlessingsMacaroon: ", err))
		return
	}
	// Construct the url to send back to the tool.
	baseURL, err := validLoopbackURL(inputMacaroon.ToolRedirectURL)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("invalid ToolRedirectURL: ", err))
		return
	}
	params := url.Values{}
	params.Add("macaroon", string(util.NewMacaroon(h.args.MacaroonKey, buf.Bytes())))
	params.Add("state", inputMacaroon.ToolState)
	params.Add("object_name", h.args.MacaroonBlessingService)
	baseURL.RawQuery = params.Encode()
	http.Redirect(w, r, baseURL.String(), http.StatusFound)
}

func (h *handler) caveats(r *http.Request) ([]security.Caveat, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	var caveats []security.Caveat
	var caveat security.Caveat
	var err error

	userProvidedExpiryCaveat := false

	for i, cavName := range r.Form["caveat"] {
		if cavName == "none" {
			continue
		}
		if cavName == "ExpiryCaveat" {
			userProvidedExpiryCaveat = true
		}
		args := strings.Split(r.Form[cavName][i], ",")
		cavInfo, ok := caveatMap[cavName]
		if !ok {
			return nil, fmt.Errorf("unable to create caveat %s: caveat does not exist", cavName)
		}
		caveat, err := cavInfo.New(args...)
		if err != nil {
			return nil, fmt.Errorf("unable to create caveat %s(%v): cavInfo.New failed: %v", cavName, args, err)
		}
		caveats = append(caveats, caveat)
	}

	// TODO(suharshs): have a checkbox in the form that says "include revocation caveat".
	if h.args.RevocationManager != nil {
		revocationCaveat, err := h.args.RevocationManager.NewCaveat(h.args.R.Identity().PublicID(), h.args.DischargerLocation)
		if err != nil {
			return nil, err
		}
		caveat, err = security.NewCaveat(revocationCaveat)
	} else if !userProvidedExpiryCaveat {
		caveat, err = security.ExpiryCaveat(time.Now().Add(h.args.BlessingDuration))
	}
	if err != nil {
		return nil, err
	}
	// revocationCaveat need to be prepended for extraction in ReadBlessAuditEntry.
	caveats = append([]security.Caveat{caveat}, caveats...)

	return caveats, nil
}

type caveatInfo struct {
	New         func(args ...string) (security.Caveat, error)
	Placeholder string
}

// caveatMap is a map from Caveat name to caveat information.
// To add to this map append
// key = "CaveatName"
// New = func that returns instantiation of specific caveat wrapped in security.Caveat.
// Placeholder = the placeholder text for the html input element.
var caveatMap = map[string]caveatInfo{
	"ExpiryCaveat": {
		New: func(args ...string) (security.Caveat, error) {
			if len(args) != 1 {
				return security.Caveat{}, fmt.Errorf("must pass exactly one duration string.")
			}
			dur, err := time.ParseDuration(args[0])
			if err != nil {
				return security.Caveat{}, fmt.Errorf("parse duration failed: %v", err)
			}
			return security.ExpiryCaveat(time.Now().Add(dur))
		},
		Placeholder: "i.e. 2h45m. Valid time units are ns, us (or µs), ms, s, m, h.",
	},
	"MethodCaveat": {
		New: func(args ...string) (security.Caveat, error) {
			if len(args) < 1 {
				return security.Caveat{}, fmt.Errorf("must pass at least one method")
			}
			return security.MethodCaveat(args[0], args[1:]...)
		},
		Placeholder: "Comma-separated method names.",
	},
	"PeerBlessingsCaveat": {
		New: func(args ...string) (security.Caveat, error) {
			if len(args) < 1 {
				return security.Caveat{}, fmt.Errorf("must pass at least one blessing pattern")
			}
			var patterns []security.BlessingPattern
			for _, arg := range args {
				patterns = append(patterns, security.BlessingPattern(arg))
			}
			return security.PeerBlessingsCaveat(patterns[0], patterns[1:]...)
		},
		Placeholder: "Comma-separated blessing patterns.",
	},
}

// exchangeAuthCodeForEmail exchanges the authorization code (which must
// have been obtained with scope=email) for an OAuth token and then uses Google's
// tokeninfo API to extract the email address from that token.
func exchangeAuthCodeForEmail(config *oauth.Config, authcode string) (string, error) {
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
