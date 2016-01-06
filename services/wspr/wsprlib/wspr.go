// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package wsprlib implements utilities for the wspr web socket proxy, which
// converts between the Vanadium RPC protocol and a custom web socket based
// protocol.
package wsprlib

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"

	"v.io/x/ref/services/wspr/internal/account"
	"v.io/x/ref/services/wspr/internal/principal"
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
	listenSpec       *rpc.ListenSpec
	namespaceRoots   []string
	principalManager *principal.PrincipalManager
	accountManager   *account.AccountManager
	pipes            map[*http.Request]*pipe
}

// Starts listening for requests and returns the network endpoint address.
func (wspr *WSPR) Listen() net.Addr {
	addr := fmt.Sprintf("127.0.0.1:%d", wspr.httpPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		wspr.ctx.Fatalf("Listen failed: %s", err)
	}
	wspr.ln = ln.(*net.TCPListener)
	wspr.ctx.VI(1).Infof("Listening at %s", ln.Addr().String())
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
	http.HandleFunc("/ws", wspr.handleWS)
	// Everything else is a 404.
	// Note: the pattern "/" matches all paths not matched by other registered
	// patterns, not just the URL with Path == "/".
	// (http://golang.org/pkg/net/http/#ServeMux)
	http.Handle("/", http.NotFoundHandler())

	if err := http.Serve(tcpKeepAliveListener{wspr.ln}, nil); err != nil {
		wspr.ctx.Fatalf("Serve failed: %s", err)
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
func NewWSPR(ctx *context.T, httpPort int, listenSpec *rpc.ListenSpec, identdEP string, namespaceRoots []string) *WSPR {
	if listenSpec.Proxy == "" {
		ctx.Fatalf("a vanadium proxy must be set")
	}

	wspr := &WSPR{
		ctx:            ctx,
		httpPort:       httpPort,
		listenSpec:     listenSpec,
		namespaceRoots: namespaceRoots,
		pipes:          map[*http.Request]*pipe{},
	}

	p := v23.GetPrincipal(ctx)
	var err error
	// TODO(nlacasse): Use a serializer that can actually persist, as we do in browspr.
	if wspr.principalManager, err = principal.NewPrincipalManager(p, principal.NewInMemorySerializer()); err != nil {
		ctx.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	wspr.accountManager = account.NewAccountManager(identdEP, wspr.principalManager)

	return wspr
}

func (wspr *WSPR) logAndSendBadReqErr(w http.ResponseWriter, msg string) {
	wspr.ctx.Error(msg)
	http.Error(w, msg, http.StatusBadRequest)
	return
}

// HTTP Handlers

func (wspr *WSPR) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	wspr.ctx.VI(0).Info("Creating a new websocket")
	p := newPipe(w, r, wspr, nil)

	if p == nil {
		return
	}
	wspr.mu.Lock()
	defer wspr.mu.Unlock()
	wspr.pipes[r] = p
}
