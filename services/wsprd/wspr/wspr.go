// A simple WebSocket proxy (WSPR) that takes in a Veyron RPC message, encoded in JSON
// and stored in a WebSocket message, and sends it to the specified Veyron
// endpoint.
//
// Input arguments must be provided as a JSON message in the following format:
//
// {
//   "Address" : String, //EndPoint Address
//   "Name" : String, //Service Name
//   "Method"   : String, //Method Name
//   "InArgs"     : { "ArgName1" : ArgVal1, "ArgName2" : ArgVal2, ... },
//   "IsStreaming" : true/false
// }
//
package wspr

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/wspr/veyron/services/wsprd/identity"
	"veyron.io/wspr/veyron/services/wsprd/principal"
)

const (
	pingInterval = 50 * time.Second              // how often the server pings the client.
	pongTimeout  = pingInterval + 10*time.Second // maximum wait for pong.
)

type blesserService interface {
	BlessUsingAccessToken(ctx context.T, token string, opts ...ipc.CallOpt) (blessingObj vdlutil.Any, blessings []string, err error)
}

type bs struct {
	client ipc.Client
	name   string
}

func (s *bs) BlessUsingAccessToken(ctx context.T, token string, opts ...ipc.CallOpt) (blessingObj vdlutil.Any, blessings []string, err error) {
	var call ipc.Call
	if call, err = s.client.StartCall(ctx, s.name, "BlessUsingAccessToken", []interface{}{token}, opts...); err != nil {
		return
	}
	var email string
	if ierr := call.Finish(&blessingObj, &email, &err); ierr != nil {
		err = ierr
	}
	serverBlessings, _ := call.RemoteBlessings()
	for _, b := range serverBlessings {
		blessings = append(blessings, b+security.ChainSeparator+email)
	}
	return
}

type wsprConfig struct {
	MounttableRoot []string
}

type WSPR struct {
	mu               sync.Mutex
	tlsCert          *tls.Certificate
	rt               veyron2.Runtime
	httpPort         int // HTTP port for WSPR to serve on. Port rather than address to discourage serving in a way that isn't local.
	logger           vlog.Logger
	listenSpec       ipc.ListenSpec
	identdEP         string
	principalManager *principal.PrincipalManager
	blesser          blesserService
	pipes            map[*http.Request]*pipe

	// TODO(ataly, ashankar): Get rid of the fields below once the old
	// security model is killed.
	useOldModel bool
	idManager   *identity.IDManager
}

var logger vlog.Logger

func readFromRequest(r *http.Request) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	if readBytes, err := io.Copy(&buf, r.Body); err != nil {
		return nil, fmt.Errorf("error copying message out of request: %v", err)
	} else if wantBytes := r.ContentLength; readBytes != wantBytes {
		return nil, fmt.Errorf("read %d bytes, wanted %d", readBytes, wantBytes)
	}
	return &buf, nil
}

// Starts the proxy and listens for requests. This method is blocking.
func (ctx WSPR) Run() {
	// Initialize the Blesser service
	ctx.blesser = &bs{client: ctx.rt.Client(), name: ctx.identdEP}

	// HTTP routes
	http.HandleFunc("/debug", ctx.handleDebug)
	http.HandleFunc("/create-account", ctx.handleCreateAccount)
	http.HandleFunc("/assoc-account", ctx.handleAssocAccount)
	http.HandleFunc("/ws", ctx.handleWS)
	// Everything else is a 404.
	// Note: the pattern "/" matches all paths not matched by other
	// registered patterns, not just the URL with Path == "/".'
	// (http://golang.org/pkg/net/http/#ServeMux)
	http.Handle("/", http.NotFoundHandler())
	ctx.logger.VI(1).Infof("Listening at port %d.", ctx.httpPort)
	httpErr := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", ctx.httpPort), nil)
	if httpErr != nil {
		log.Fatalf("Failed to HTTP serve: %s", httpErr)
	}
}

func (ctx WSPR) Shutdown() {
	ctx.rt.Cleanup()
}

func (ctx WSPR) CleanUpPipe(req *http.Request) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	delete(ctx.pipes, req)
}

// Creates a new WebSocket Proxy object.
func NewWSPR(httpPort int, listenSpec ipc.ListenSpec, identdEP string, opts ...veyron2.ROpt) *WSPR {
	if listenSpec.Proxy == "" {
		log.Fatalf("a veyron proxy must be set")
	}
	if identdEP == "" {
		log.Fatalf("an identd server must be set")
	}

	newrt, err := rt.New(opts...)
	if err != nil {
		log.Fatalf("rt.New failed: %s", err)
	}

	wspr := &WSPR{
		httpPort:    httpPort,
		listenSpec:  listenSpec,
		identdEP:    identdEP,
		rt:          newrt,
		logger:      newrt.Logger(),
		pipes:       map[*http.Request]*pipe{},
		useOldModel: true,
	}

	for _, o := range opts {
		if _, ok := o.(options.ForceNewSecurityModel); ok {
			wspr.useOldModel = false
			break
		}
	}

	if wspr.useOldModel {
		if wspr.idManager, err = identity.NewIDManager(newrt, &identity.InMemorySerializer{}); err != nil {
			log.Fatalf("identity.NewIDManager failed: %s", err)
		}
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	if wspr.principalManager, err = principal.NewPrincipalManager(newrt.Principal(), &principal.InMemorySerializer{}); err != nil {
		log.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	return wspr
}

func (ctx WSPR) logAndSendBadReqErr(w http.ResponseWriter, msg string) {
	ctx.logger.Error(msg)
	http.Error(w, msg, http.StatusBadRequest)
	return
}

// HTTP Handlers

func (ctx WSPR) handleDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, "")
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<html>
<head>
<title>/debug</title>
</head>
<body>
<ul>
<li><a href="/debug/pprof">/debug/pprof</a></li>
</li></ul></body></html>
`))
}

func (ctx WSPR) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	ctx.logger.VI(0).Info("Creating a new websocket")
	p := newPipe(w, r, &ctx, nil)

	if p == nil {
		return
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.pipes[r] = p
}

// Structs for marshalling input/output to create-account route.
type createAccountInput struct {
	AccessToken string `json:"access_token"`
}

type createAccountOutput struct {
	Names []string `json:"names"`
}

// Handler for creating an account in the principal manager.
// A valid OAuth2 access token must be supplied in the request body,
// which is exchanged for blessings from the veyron blessing server.
// An account based on the blessings is then added to WSPR's principal
// manager, and the set of blessing strings are returned to the client.
func (ctx WSPR) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body.
	var data createAccountInput
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error parsing body: %v", err))
	}

	// Get a blessing for the access token from identity server.
	rctx, cancel := ctx.rt.NewContext().WithTimeout(time.Minute)
	defer cancel()
	blessingsAny, blessings, err := ctx.blesser.BlessUsingAccessToken(rctx, data.AccessToken)
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error getting blessing for access token: %v", err))
		return
	}

	// Shortcut for old security model.
	if ctx.useOldModel {
		ctx.handleCreateAccountOldModel(blessingsAny, w)
		return
	}

	accountBlessings, err := security.NewBlessings(blessingsAny.(security.WireBlessings))
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error creating blessings from wire data: %v", err))
		return
	}
	// Add accountBlessings to principalManager under each of the
	// returned blessing strings.
	// TODO(ataly, ashankar): Adding the same account under different
	// different names seems a little weird. Figure out a cleaner way
	// of adding the account.
	for _, b := range blessings {
		if err := ctx.principalManager.AddAccount(b, accountBlessings); err != nil {
			ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error adding account: %v", err))
			return
		}
	}

	// Return blessings to the client.
	out := createAccountOutput{
		Names: blessings,
	}
	outJson, err := json.Marshal(out)
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error mashalling names: %v", err))
		return
	}

	// Success.
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(outJson))
}

// Struct for marshalling input to assoc-account route.
type assocAccountInput struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
}

// Handler for associating an existing principal with an origin.
func (ctx WSPR) handleAssocAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body.
	var data assocAccountInput
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, fmt.Sprintf("Error parsing body: %v", err), http.StatusBadRequest)
	}

	// Shortcup for old security model.
	if ctx.useOldModel {
		ctx.handleAssocAccountOldModel(data, w)
		return
	}

	// Store the origin.
	// TODO(nlacasse, bjornick): determine what the caveats should be.
	if err := ctx.principalManager.AddOrigin(data.Origin, data.Name, nil); err != nil {
		http.Error(w, fmt.Sprintf("Error associating account: %v", err), http.StatusBadRequest)
		return
	}

	// Success.
	fmt.Fprintf(w, "")
}

// TODO(ataly, ashankar): Remove this method once the old security model is killed.
func (ctx WSPR) handleCreateAccountOldModel(blessingsAny vdlutil.Any, w http.ResponseWriter) {
	blessing, ok := blessingsAny.(security.PublicID)
	if !ok {
		ctx.logAndSendBadReqErr(w, "Error creating PublicID from wire data")
		return
	}

	// Derive a new identity from the runtime's identity and the blessing.
	identity, err := ctx.rt.Identity().Derive(blessing)
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error deriving identity: %v", err))
		return
	}

	for _, name := range blessing.Names() {
		// Store identity in identity manager.
		if err := ctx.idManager.AddAccount(name, identity); err != nil {
			ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error storing identity: %v", err))
			return
		}
	}

	// Return the names to the client.
	out := createAccountOutput{
		Names: blessing.Names(),
	}
	outJson, err := json.Marshal(out)
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error mashalling names: %v", err))
		return
	}

	// Success.
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(outJson))
}

// TODO(ataly, ashankar): Remove this method once the old security model is killed.
func (ctx WSPR) handleAssocAccountOldModel(data assocAccountInput, w http.ResponseWriter) {
	if err := ctx.idManager.AddOrigin(data.Origin, data.Name, nil); err != nil {
		http.Error(w, fmt.Sprintf("Error associating account: %v", err), http.StatusBadRequest)
		return
	}

	// Success.
	fmt.Fprintf(w, "")
}
