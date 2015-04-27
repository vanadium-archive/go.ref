// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon proxyd listens for connections from Vanadium services (typically
// behind NATs) and proxies these services to the outside world.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"time"

	"v.io/v23"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	"v.io/x/ref/profiles/static"
)

var (
	pubAddress  = flag.String("published-address", "", "deprecated - the proxy now uses listenspecs and the address chooser mechanism")
	healthzAddr = flag.String("healthz-address", "", "Network address on which the HTTP healthz server runs. It is intended to be used with a load balancer. The load balancer must be able to reach this address in order to verify that the proxy server is running")
	name        = flag.String("name", "", "Name to mount the proxy as")
	acl         = flag.String("access-list", "", "Blessings that are authorized to listen via the proxy. JSON-encoded representation of access.AccessList. An empty string implies the default authorization policy.")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	listenSpec := v23.GetListenSpec(ctx)
	if len(listenSpec.Addrs) != 1 {
		vlog.Fatalf("proxyd can only listen on one address: %v", listenSpec.Addrs)
	}
	if listenSpec.Proxy != "" {
		vlog.Fatalf("proxyd cannot listen through another proxy")
	}
	var authorizer security.Authorizer
	if len(*acl) > 0 {
		var list access.AccessList
		if err := json.NewDecoder(bytes.NewBufferString(*acl)).Decode(&list); err != nil {
			vlog.Fatalf("invalid --access-list: %v", err)
		}
		// Always add ourselves, for the the reserved methods server
		// started below.
		list.In = append(list.In, security.DefaultBlessingPatterns(v23.GetPrincipal(ctx))...)
		vlog.Infof("Using access list to control proxy use: %v", list)
		authorizer = list
	}

	proxyShutdown, proxyEndpoint, err := static.NewProxy(ctx, listenSpec, authorizer, *name)
	if err != nil {
		vlog.Fatal(err)
	}
	defer proxyShutdown()

	if len(*name) > 0 {
		// Print out a directly accessible name for the proxy table so
		// that integration tests can reliably read it from stdout.
		fmt.Printf("NAME=%s\n", proxyEndpoint.Name())
	} else {
		fmt.Printf("Proxy listening on %s\n", proxyEndpoint)
	}

	if len(*healthzAddr) != 0 {
		go startHealthzServer(*healthzAddr)
	}

	// Start an RPC Server that listens through the proxy itself. This
	// server will serve reserved methods only.
	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()
	ls := rpc.ListenSpec{Proxy: proxyEndpoint.Name()}
	if _, err := server.Listen(ls); err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", ls, err)
	}
	var monitoringName string
	if len(*name) > 0 {
		monitoringName = *name + "-mon"
	}
	if err := server.ServeDispatcher(monitoringName, &nilDispatcher{}); err != nil {
		vlog.Fatalf("ServeDispatcher(%v) failed: %v", monitoringName, err)
	}

	<-signals.ShutdownOnSignals(ctx)
}

type nilDispatcher struct{}

func (nilDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return nil, nil, nil
}

// healthzHandler implements net/http.Handler
type healthzHandler struct{}

func (healthzHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

// startHealthzServer starts a HTTP server that simply returns "ok" to every
// request. This is needed to let the load balancer know that the proxy server
// is running.
func startHealthzServer(addr string) {
	s := http.Server{
		Addr:         addr,
		Handler:      healthzHandler{},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	if err := s.ListenAndServe(); err != nil {
		vlog.Fatal(err)
	}
}
