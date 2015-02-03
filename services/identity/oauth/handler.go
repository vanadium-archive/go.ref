// Package oauth implements an http.Handler that has two main purposes
// listed below:
//
// (1) Uses OAuth to authenticate and then renders a page that
//     displays all the blessings that were provided for that Google user.
//     The client calls the /listblessings route which redirects to listblessingscallback which
//     renders the list.
// (2) Performs the oauth flow for seeking a blessing using the principal tool
//     located at veyron/tools/principal.
//     The seek blessing flow works as follows:
//     (a) Client (principal tool) hits the /seekblessings route.
//     (b) /seekblessings performs oauth with a redirect to /seekblessingscallback.
//     (c) Client specifies desired caveats in the form that /seekblessingscallback displays.
//     (d) Submission of the form sends caveat information to /sendmacaroon.
//     (e) /sendmacaroon sends a macaroon with blessing information to client
//         (via a redirect to an HTTP server run by the tool).
//     (f) Client invokes bless rpc with macaroon.

package oauth

import (
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

	"v.io/core/veyron/services/identity/auditor"
	"v.io/core/veyron/services/identity/caveats"
	"v.io/core/veyron/services/identity/revocation"
	"v.io/core/veyron/services/identity/util"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vom"
)

const (
	clientIDCookie = "VeyronHTTPIdentityClientID"

	ListBlessingsRoute         = "listblessings"
	listBlessingsCallbackRoute = "listblessingscallback"
	revokeRoute                = "revoke"
	SeekBlessingsRoute         = "seekblessings"
	addCaveatsRoute            = "addcaveats"
	sendMacaroonRoute          = "sendmacaroon"
)

type HandlerArgs struct {
	// The principal to use.
	Principal security.Principal
	// The Key that is used for creating and verifying macaroons.
	// This needs to be common between the handler and the MacaroonBlesser service.
	MacaroonKey []byte
	// URL at which the hander is installed.
	// e.g. http://host:port/google/
	Addr string
	// BlessingLogReder is needed for reading audit logs.
	BlessingLogReader auditor.BlessingLogReader
	// The RevocationManager is used to revoke blessings granted with a revocation caveat.
	// If nil, then revocation caveats cannot be added to blessings and an expiration caveat
	// will be used instead.
	RevocationManager revocation.RevocationManager
	// The object name of the discharger service.
	DischargerLocation string
	// MacaroonBlessingService is the object name to which macaroons create by this HTTP
	// handler can be exchanged for a blessing.
	MacaroonBlessingService string
	// EmailClassifier is used to decide the prefix used for blessing extensions.
	// For example, if EmailClassifier.Classify("foo@bar.com") returns "guests",
	// then the email foo@bar.com will receive the blessing "guests/foo@bar.com".
	EmailClassifier *util.EmailClassifier
	// OAuthProvider is used to authenticate and get a blessee email.
	OAuthProvider OAuthProvider
	// CaveatSelector is used to obtain caveats from the user when seeking a blessing.
	CaveatSelector caveats.CaveatSelector
}

// BlessingMacaroon contains the data that is encoded into the macaroon for creating blessings.
type BlessingMacaroon struct {
	Creation time.Time
	Caveats  []security.Caveat
	Name     string
}

func redirectURL(baseURL, suffix string) string {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return baseURL + suffix
}

// NewHandler returns an http.Handler that expects to be rooted at args.Addr
// and can be used to authenticate with args.OAuthProvider, mint a new
// identity and bless it with the OAuthProvider email address.
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
	http.Redirect(w, r, h.args.OAuthProvider.AuthURL(redirectURL(h.args.Addr, listBlessingsCallbackRoute), csrf), http.StatusFound)
}

func (h *handler) listBlessingsCallback(w http.ResponseWriter, r *http.Request) {
	if err := h.csrfCop.ValidateToken(r.FormValue("state"), r, clientIDCookie, nil); err != nil {
		vlog.Infof("Invalid CSRF token: %v in request: %#v", err, r)
		util.HTTPBadRequest(w, r, fmt.Errorf("Suspected request forgery: %v", err))
		return
	}
	email, err := h.args.OAuthProvider.ExchangeAuthCodeForEmail(r.FormValue("code"), redirectURL(h.args.Addr, listBlessingsCallbackRoute))
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}

	type tmplentry struct {
		Timestamp      time.Time
		Caveats        []security.Caveat
		RevocationTime time.Time
		Blessed        security.Blessings
		Token          string
		Error          error
	}
	tmplargs := struct {
		Log                chan tmplentry
		Email, RevokeRoute string
	}{
		Log:         make(chan tmplentry),
		Email:       email,
		RevokeRoute: revokeRoute,
	}
	entrych := h.args.BlessingLogReader.Read(email)

	w.Header().Set("Context-Type", "text/html")
	// This MaybeSetCookie call is needed to ensure that a cookie is created. Since the
	// header cannot be changed once the body is written to, this needs to be called first.
	if _, err = h.csrfCop.MaybeSetCookie(w, r, clientIDCookie); err != nil {
		vlog.Infof("Failed to set CSRF cookie[%v] for request %#v", err, r)
		util.HTTPServerError(w, err)
		return
	}
	go func(ch chan tmplentry) {
		defer close(ch)
		for entry := range entrych {
			tmplEntry := tmplentry{
				Error:     entry.DecodeError,
				Timestamp: entry.Timestamp,
				Caveats:   entry.Caveats,
				Blessed:   entry.Blessings,
			}
			if len(entry.RevocationCaveatID) > 0 && h.args.RevocationManager != nil {
				if revocationTime := h.args.RevocationManager.GetRevocationTime(entry.RevocationCaveatID); revocationTime != nil {
					tmplEntry.RevocationTime = *revocationTime
				} else {
					caveatID := base64.URLEncoding.EncodeToString([]byte(entry.RevocationCaveatID))
					if tmplEntry.Token, err = h.csrfCop.NewToken(w, r, clientIDCookie, caveatID); err != nil {
						vlog.Errorf("Failed to create CSRF token[%v] for request %#v", err, r)
						tmplEntry.Error = fmt.Errorf("server error: unable to create revocation token")
					}
				}
			}
			ch <- tmplEntry
		}
	}(tmplargs.Log)
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
	if err := h.csrfCop.ValidateToken(Token, r, clientIDCookie, &encCaveatID); err != nil {
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
	http.Redirect(w, r, h.args.OAuthProvider.AuthURL(redirectURL(h.args.Addr, addCaveatsRoute), outputMacaroon), http.StatusFound)
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
	email, err := h.args.OAuthProvider.ExchangeAuthCodeForEmail(r.FormValue("code"), redirectURL(h.args.Addr, addCaveatsRoute))
	if err != nil {
		util.HTTPBadRequest(w, r, err)
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
	if err := h.args.CaveatSelector.Render(email, outputMacaroon, redirectURL(h.args.Addr, sendMacaroonRoute), w, r); err != nil {
		vlog.Errorf("Unable to invoke render caveat selector: %v", err)
		util.HTTPServerError(w, err)
	}
}

func (h *handler) sendMacaroon(w http.ResponseWriter, r *http.Request) {
	var inputMacaroon addCaveatsMacaroon
	caveatInfos, macaroonString, blessingExtension, err := h.args.CaveatSelector.ParseSelections(r)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to parse blessing information: %v", err))
		return
	}
	if err := h.csrfCop.ValidateToken(macaroonString, r, clientIDCookie, &inputMacaroon); err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("suspected request forgery: %v", err))
		return
	}

	caveats, err := h.caveats(caveatInfos)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to create caveats: %v", err))
		return
	}
	parts := []string{
		h.args.EmailClassifier.Classify(inputMacaroon.Email),
		inputMacaroon.Email,
	}
	if len(blessingExtension) > 0 {
		parts = append(parts, blessingExtension)
	}
	if len(caveats) == 0 {
		util.HTTPBadRequest(w, r, fmt.Errorf("server disallows attempts to bless with no caveats"))
		return
	}
	m := BlessingMacaroon{
		Creation: time.Now(),
		Caveats:  caveats,
		Name:     strings.Join(parts, security.ChainSeparator),
	}
	macBytes, err := vom.Encode(m)
	if err != nil {
		util.HTTPServerError(w, fmt.Errorf("failed to encode BlessingsMacaroon: %v", err))
		return
	}
	// Construct the url to send back to the tool.
	baseURL, err := validLoopbackURL(inputMacaroon.ToolRedirectURL)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("invalid ToolRedirectURL: %v", err))
		return
	}
	params := url.Values{}
	params.Add("macaroon", string(util.NewMacaroon(h.args.MacaroonKey, macBytes)))
	params.Add("state", inputMacaroon.ToolState)
	params.Add("object_name", h.args.MacaroonBlessingService)
	baseURL.RawQuery = params.Encode()
	http.Redirect(w, r, baseURL.String(), http.StatusFound)
}

func (h *handler) caveats(caveatInfos []caveats.CaveatInfo) (cavs []security.Caveat, err error) {
	caveatFactories := caveats.NewCaveatFactory()
	for _, caveatInfo := range caveatInfos {
		if caveatInfo.Type == "Revocation" {
			caveatInfo.Args = []interface{}{h.args.RevocationManager, h.args.Principal.PublicKey(), h.args.DischargerLocation}
		}
		cav, err := caveatFactories.New(caveatInfo)
		if err != nil {
			return nil, err
		}
		cavs = append(cavs, cav)
	}
	return
}
