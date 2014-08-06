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
	"log"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"time"

	"veyron/services/wsprd/identity"
	"veyron2"
	"veyron2/rt"
	"veyron2/vlog"
)

const (
	pingInterval = 50 * time.Second              // how often the server pings the client.
	pongTimeout  = pingInterval + 10*time.Second // maximum wait for pong.
)

type wsprConfig struct {
	MounttableRoot []string
}

type WSPR struct {
	mu            sync.Mutex
	tlsCert       *tls.Certificate
	rt            veyron2.Runtime
	logger        vlog.Logger
	port          int
	veyronProxyEP string
	idManager     *identity.IDManager
	pipes         map[*http.Request]*pipe
}

var logger vlog.Logger

func (ctx WSPR) handleDebug(w http.ResponseWriter, r *http.Request) {
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

func readFromRequest(r *http.Request) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	if readBytes, err := io.Copy(&buf, r.Body); err != nil {
		return nil, fmt.Errorf("error copying message out of request: %v", err)
	} else if wantBytes := r.ContentLength; readBytes != wantBytes {
		return nil, fmt.Errorf("read %d bytes, wanted %d", readBytes, wantBytes)
	}
	return &buf, nil
}

func setAccessControl(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// Starts the proxy and listens for requests. This method is blocking.
func (ctx WSPR) Run() {
	http.HandleFunc("/debug", ctx.handleDebug)
	http.Handle("/favicon.ico", http.NotFoundHandler())
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ctx.logger.VI(0).Info("Creating a new websocket")
		p := newPipe(w, r, &ctx, nil)

		if p == nil {
			return
		}
		ctx.mu.Lock()
		defer ctx.mu.Unlock()
		ctx.pipes[r] = p
	})
	ctx.logger.VI(1).Infof("Listening on port %d.", ctx.port)
	httpErr := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", ctx.port), nil)
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
func NewWSPR(port int, veyronProxyEP string, opts ...veyron2.ROpt) *WSPR {
	if veyronProxyEP == "" {
		log.Fatalf("a veyron proxy must be set")
	}

	newrt, err := rt.New(opts...)
	if err != nil {
		log.Fatalf("rt.New failed: %s", err)
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	idManager, err := identity.NewIDManager(newrt, &identity.InMemorySerializer{})
	if err != nil {
		log.Fatalf("identity.NewIDManager failed: %s", err)
	}

	return &WSPR{port: port,
		veyronProxyEP: veyronProxyEP,
		rt:            newrt,
		logger:        newrt.Logger(),
		idManager:     idManager,
	}
}
