// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/logging"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/runtime/factories/roaming"
)

var pubAddress, healthzAddr, name, acl string

func main() {
	cmdProxyD.Flags.StringVar(&pubAddress, "published-address", "", "DEPRECATED - the proxy now uses listenspecs and the address chooser mechanism.")
	cmdProxyD.Flags.StringVar(&healthzAddr, "healthz-address", "", "Network address on which the HTTP healthz server runs.  It is intended to be used with a load balancer.  The load balancer must be able to reach this address in order to verify that the proxy server is running.")
	cmdProxyD.Flags.StringVar(&name, "name", "", "Name to mount the proxy as.")
	cmdProxyD.Flags.StringVar(&acl, "access-list", "", "Blessings that are authorized to listen via the proxy.  JSON-encoded representation of access.AccessList.  An empty string implies the default authorization policy.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdProxyD)
}

var cmdProxyD = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runProxyD),
	Name:   "proxyd",
	Short:  "Proxies services to the outside world",
	Long: `
Command proxyd is a daemon that listens for connections from Vanadium services
(typically behind NATs) and proxies these services to the outside world.
`,
}

func runProxyD(ctx *context.T, env *cmdline.Env, args []string) error {
	listenSpec := v23.GetListenSpec(ctx)
	if len(listenSpec.Addrs) != 1 {
		return env.UsageErrorf("proxyd can only listen on one address: %v", listenSpec.Addrs)
	}
	if listenSpec.Proxy != "" {
		return env.UsageErrorf("proxyd cannot listen through another proxy")
	}
	var authorizer security.Authorizer
	if len(acl) > 0 {
		var list access.AccessList
		if err := json.NewDecoder(bytes.NewBufferString(acl)).Decode(&list); err != nil {
			return env.UsageErrorf("invalid -access-list: %v", err)
		}
		// Always add ourselves, for the the reserved methods server
		// started below.
		list.In = append(list.In, security.DefaultBlessingPatterns(v23.GetPrincipal(ctx))...)
		ctx.Infof("Using access list to control proxy use: %v", list)
		authorizer = list
	}

	proxyShutdown, proxyEndpoint, err := roaming.NewProxy(ctx, listenSpec, authorizer, name)
	if err != nil {
		return err
	}
	defer proxyShutdown()

	if len(name) > 0 {
		// Print out a directly accessible name for the proxy table so
		// that integration tests can reliably read it from stdout.
		fmt.Printf("NAME=%s\n", proxyEndpoint.Name())
	} else {
		fmt.Printf("Proxy listening on %s\n", proxyEndpoint)
	}

	if len(healthzAddr) != 0 {
		go startHealthzServer(ctx, healthzAddr)
	}

	<-signals.ShutdownOnSignals(ctx)
	return nil
}

type nilDispatcher struct{}

func (nilDispatcher) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
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
func startHealthzServer(logger logging.Logger, addr string) {
	s := http.Server{
		Addr:         addr,
		Handler:      healthzHandler{},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	if err := s.ListenAndServe(); err != nil {
		logger.Fatal(err)
	}
}
