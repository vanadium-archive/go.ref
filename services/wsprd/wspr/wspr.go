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
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/wspr/veyron/services/wsprd/principal"
)

const (
	pingInterval = 50 * time.Second              // how often the server pings the client.
	pongTimeout  = pingInterval + 10*time.Second // maximum wait for pong.
)

type blesserService interface {
	BlessUsingAccessToken(ctx context.T, token string, opts ...ipc.CallOpt) (blessingObj vdlutil.Any, account string, err error)
}

type bs struct {
	client ipc.Client
	name   string
}

func (s *bs) BlessUsingAccessToken(ctx context.T, token string, opts ...ipc.CallOpt) (blessingObj vdlutil.Any, account string, err error) {
	var call ipc.Call
	if call, err = s.client.StartCall(ctx, s.name, "BlessUsingAccessToken", []interface{}{token}, opts...); err != nil {
		return
	}
	var email string
	if ierr := call.Finish(&blessingObj, &email, &err); ierr != nil {
		err = ierr
	}
	serverBlessings, _ := call.RemoteBlessings()
	return blessingObj, accountName(serverBlessings, email), nil
}

func accountName(serverBlessings []string, email string) string {
	return strings.Join(serverBlessings, "#") + security.ChainSeparator + email
}

type WSPR struct {
	mu      sync.Mutex
	tlsCert *tls.Certificate
	rt      veyron2.Runtime
	// HTTP port for WSPR to serve on. Note, WSPR always serves on localhost.
	httpPort         int
	ln               *net.TCPListener // HTTP listener
	logger           vlog.Logger
	listenSpec       ipc.ListenSpec
	identdEP         string
	principalManager *principal.PrincipalManager
	blesser          blesserService
	pipes            map[*http.Request]*pipe
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

// Starts listening for requests and returns the network endpoint address.
func (ctx *WSPR) Listen() net.Addr {
	addr := fmt.Sprintf("127.0.0.1:%d", ctx.httpPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		vlog.Fatalf("Listen failed: %s", err)
	}
	ctx.ln = ln.(*net.TCPListener)
	ctx.logger.VI(1).Infof("Listening at %s", ln.Addr().String())
	return ln.Addr()
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted connections.
// It's used by ListenAndServe and ListenAndServeTLS so dead TCP connections
// (e.g. closing laptop mid-download) eventually go away.
// Copied from http/server.go, since it's not exported.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

// Starts serving http requests. This method is blocking.
func (ctx *WSPR) Serve() {
	// Initialize the Blesser service.
	ctx.blesser = &bs{client: ctx.rt.Client(), name: ctx.identdEP}

	// Configure HTTP routes.
	http.HandleFunc("/debug", ctx.handleDebug)
	http.HandleFunc("/create-account", ctx.handleCreateAccount)
	http.HandleFunc("/assoc-account", ctx.handleAssocAccount)
	http.HandleFunc("/ws", ctx.handleWS)
	// Everything else is a 404.
	// Note: the pattern "/" matches all paths not matched by other registered
	// patterns, not just the URL with Path == "/".
	// (http://golang.org/pkg/net/http/#ServeMux)
	http.Handle("/", http.NotFoundHandler())

	if err := http.Serve(tcpKeepAliveListener{ctx.ln}, nil); err != nil {
		vlog.Fatalf("Serve failed: %s", err)
	}
}

func (ctx *WSPR) Shutdown() {
	ctx.rt.Cleanup()
}

func (ctx *WSPR) CleanUpPipe(req *http.Request) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	delete(ctx.pipes, req)
}

// Creates a new WebSocket Proxy object.
func NewWSPR(httpPort int, listenSpec ipc.ListenSpec, identdEP string, opts ...veyron2.ROpt) *WSPR {
	if listenSpec.Proxy == "" {
		vlog.Fatalf("a veyron proxy must be set")
	}
	if identdEP == "" {
		vlog.Fatalf("an identd server must be set")
	}

	newrt, err := rt.New(opts...)
	if err != nil {
		vlog.Fatalf("rt.New failed: %s", err)
	}

	wspr := &WSPR{
		httpPort:   httpPort,
		listenSpec: listenSpec,
		identdEP:   identdEP,
		rt:         newrt,
		logger:     newrt.Logger(),
		pipes:      map[*http.Request]*pipe{},
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	if wspr.principalManager, err = principal.NewPrincipalManager(newrt.Principal(), &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	return wspr
}

func (ctx *WSPR) logAndSendBadReqErr(w http.ResponseWriter, msg string) {
	ctx.logger.Error(msg)
	http.Error(w, msg, http.StatusBadRequest)
	return
}

// HTTP Handlers

func (ctx *WSPR) handleDebug(w http.ResponseWriter, r *http.Request) {
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

func (ctx *WSPR) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	ctx.logger.VI(0).Info("Creating a new websocket")
	p := newPipe(w, r, ctx, nil)

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
	Account string `json:"account"`
}

// Handler for creating an account in the principal manager.
// A valid OAuth2 access token must be supplied in the request body,
// which is exchanged for blessings from the veyron blessing server.
// An account based on the blessings is then added to WSPR's principal
// manager, and the set of blessing strings are returned to the client.
func (ctx *WSPR) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body.
	var data createAccountInput
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error parsing body: %v", err))
	}

	// Get a blessing for the access token from blessing server.
	rctx, cancel := ctx.rt.NewContext().WithTimeout(time.Minute)
	defer cancel()
	blessingsAny, account, err := ctx.blesser.BlessUsingAccessToken(rctx, data.AccessToken)
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error getting blessing for access token: %v", err))
		return
	}

	accountBlessings, err := security.NewBlessings(blessingsAny.(security.WireBlessings))
	if err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error creating blessings from wire data: %v", err))
		return
	}
	// Add accountBlessings to principalManager under the provided
	// account.
	if err := ctx.principalManager.AddAccount(account, accountBlessings); err != nil {
		ctx.logAndSendBadReqErr(w, fmt.Sprintf("Error adding account: %v", err))
		return
	}

	// Return blessings to the client.
	out := createAccountOutput{
		Account: account,
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
	Account string `json:"account"`
	Origin  string `json:"origin"`
}

// Handler for associating an existing principal with an origin.
func (ctx *WSPR) handleAssocAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body.
	var data assocAccountInput
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, fmt.Sprintf("Error parsing body: %v", err), http.StatusBadRequest)
	}

	// Store the origin.
	// TODO(nlacasse, bjornick): determine what the caveats should be.
	if err := ctx.principalManager.AddOrigin(data.Origin, data.Account, nil); err != nil {
		http.Error(w, fmt.Sprintf("Error associating account: %v", err), http.StatusBadRequest)
		return
	}

	// Success.
	fmt.Fprintf(w, "")
}
