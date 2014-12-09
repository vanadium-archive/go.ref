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
	"sync"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/wspr/veyron/services/wsprd/account"
	"veyron.io/wspr/veyron/services/wsprd/principal"
)

const (
	pingInterval = 50 * time.Second              // how often the server pings the client.
	pongTimeout  = pingInterval + 10*time.Second // maximum wait for pong.
)

type WSPR struct {
	mu      sync.Mutex
	tlsCert *tls.Certificate
	rt      veyron2.Runtime
	// HTTP port for WSPR to serve on. Note, WSPR always serves on localhost.
	httpPort         int
	ln               *net.TCPListener // HTTP listener
	logger           vlog.Logger
	profileFactory   func() veyron2.Profile
	listenSpec       *ipc.ListenSpec
	identdEP         string
	namespaceRoots   []string
	principalManager *principal.PrincipalManager
	accountManager   *account.AccountManager
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
	// TODO(ataly, bprosnitz): Get rid of this method if possible.
}

func (ctx *WSPR) CleanUpPipe(req *http.Request) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	delete(ctx.pipes, req)
}

// Creates a new WebSocket Proxy object.
func NewWSPR(runtime veyron2.Runtime, httpPort int, profileFactory func() veyron2.Profile, listenSpec *ipc.ListenSpec, identdEP string, namespaceRoots []string) *WSPR {
	if listenSpec.Proxy == "" {
		vlog.Fatalf("a veyron proxy must be set")
	}
	if identdEP == "" {
		vlog.Fatalf("an identd server must be set")
	}

	wspr := &WSPR{
		httpPort:       httpPort,
		profileFactory: profileFactory,
		listenSpec:     listenSpec,
		identdEP:       identdEP,
		namespaceRoots: namespaceRoots,
		rt:             runtime,
		logger:         runtime.Logger(),
		pipes:          map[*http.Request]*pipe{},
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	var err error
	if wspr.principalManager, err = principal.NewPrincipalManager(runtime.Principal(), &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	wspr.accountManager = account.NewAccountManager(runtime, identdEP, wspr.principalManager)

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

	account, err := ctx.accountManager.CreateAccount(data.AccessToken)
	if err != nil {
		ctx.logAndSendBadReqErr(w, err.Error())
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
	Account string           `json:"account"`
	Origin  string           `json:"origin"`
	Caveats []account.Caveat `json:"caveats"`
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

	if err := ctx.accountManager.AssociateAccount(data.Origin, data.Account, data.Caveats); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Success.
	fmt.Fprintf(w, "")
}
