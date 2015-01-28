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
	"fmt"
	"io"
	"net"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/vlog"

	"v.io/wspr/veyron/services/wsprd/account"
	"v.io/wspr/veyron/services/wsprd/principal"
)

const (
	pingInterval = 50 * time.Second              // how often the server pings the client.
	pongTimeout  = pingInterval + 10*time.Second // maximum wait for pong.
)

type WSPR struct {
	mu      sync.Mutex
	tlsCert *tls.Certificate
	ctx     *context.T
	// HTTP port for WSPR to serve on. Note, WSPR always serves on localhost.
	httpPort         int
	ln               *net.TCPListener // HTTP listener
	listenSpec       *ipc.ListenSpec
	namespaceRoots   []string
	principalManager *principal.PrincipalManager
	accountManager   *account.AccountManager
	pipes            map[*http.Request]*pipe
}

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
func (wspr *WSPR) Listen() net.Addr {
	addr := fmt.Sprintf("127.0.0.1:%d", wspr.httpPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		vlog.Fatalf("Listen failed: %s", err)
	}
	wspr.ln = ln.(*net.TCPListener)
	vlog.VI(1).Infof("Listening at %s", ln.Addr().String())
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
func (wspr *WSPR) Serve() {
	// Configure HTTP routes.
	http.HandleFunc("/debug", wspr.handleDebug)
	http.HandleFunc("/ws", wspr.handleWS)
	// Everything else is a 404.
	// Note: the pattern "/" matches all paths not matched by other registered
	// patterns, not just the URL with Path == "/".
	// (http://golang.org/pkg/net/http/#ServeMux)
	http.Handle("/", http.NotFoundHandler())

	if err := http.Serve(tcpKeepAliveListener{wspr.ln}, nil); err != nil {
		vlog.Fatalf("Serve failed: %s", err)
	}
}

func (wspr *WSPR) Shutdown() {
	// TODO(ataly, bprosnitz): Get rid of this method if possible.
}

func (wspr *WSPR) CleanUpPipe(req *http.Request) {
	wspr.mu.Lock()
	defer wspr.mu.Unlock()
	delete(wspr.pipes, req)
}

// Creates a new WebSocket Proxy object.
func NewWSPR(ctx *context.T, httpPort int, listenSpec *ipc.ListenSpec, identdEP string, namespaceRoots []string) *WSPR {
	if listenSpec.Proxy == "" {
		vlog.Fatalf("a veyron proxy must be set")
	}

	wspr := &WSPR{
		ctx:            ctx,
		httpPort:       httpPort,
		listenSpec:     listenSpec,
		namespaceRoots: namespaceRoots,
		pipes:          map[*http.Request]*pipe{},
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	p := veyron2.GetPrincipal(ctx)
	var err error
	if wspr.principalManager, err = principal.NewPrincipalManager(p, &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	wspr.accountManager = account.NewAccountManager(identdEP, wspr.principalManager)

	return wspr
}

func (wspr *WSPR) logAndSendBadReqErr(w http.ResponseWriter, msg string) {
	vlog.Error(msg)
	http.Error(w, msg, http.StatusBadRequest)
	return
}

// HTTP Handlers

func (wspr *WSPR) handleDebug(w http.ResponseWriter, r *http.Request) {
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

func (wspr *WSPR) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	vlog.VI(0).Info("Creating a new websocket")
	p := newPipe(w, r, wspr, nil)

	if p == nil {
		return
	}
	wspr.mu.Lock()
	defer wspr.mu.Unlock()
	wspr.pipes[r] = p
}
