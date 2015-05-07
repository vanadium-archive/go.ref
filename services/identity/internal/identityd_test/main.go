// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP server that uses OAuth to create security.Blessings objects.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"v.io/v23"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles/static"
	"v.io/x/ref/services/identity/internal/auditor"
	"v.io/x/ref/services/identity/internal/blesser"
	"v.io/x/ref/services/identity/internal/caveats"
	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/revocation"
	"v.io/x/ref/services/identity/internal/server"
	"v.io/x/ref/services/identity/internal/util"
)

var (
	// Flags controlling the HTTP server
	externalHttpAddr = flag.String("external-http-addr", "", "External address on which the HTTP server listens on. If none is provided the server will only listen on -http-addr.")
	httpAddr         = flag.String("http-addr", "localhost:0", "Address on which the HTTP server listens on.")
	tlsConfig        = flag.String("tls-config", "", "Comma-separated list of TLS certificate and private key files, in that order. This must be provided.")
	assetsPrefix     = flag.String("assets-prefix", "", "host serving the web assets for the identity server")
	mountPrefix      = flag.String("mount-prefix", "identity", "mount name prefix to use. May be rooted.")
	browser          = flag.Bool("browser", false, "whether to open a browser caveat selector")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	// Duration to use for tls cert and blessing duration.
	duration := 365 * 24 * time.Hour

	// If no tlsConfig has been provided, write and use our own.
	if flag.Lookup("tls-config").Value.String() == "" {
		addr := *externalHttpAddr
		if *externalHttpAddr == "" {
			addr = *httpAddr
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			vlog.Fatalf("Failed to parse %q: %v", httpAddr, err)
		}
		certFile, keyFile, err := util.WriteCertAndKey(host, duration)
		if err != nil {
			vlog.Fatal(err)
		}
		if err := flag.Set("tls-config", certFile+","+keyFile); err != nil {
			vlog.Fatal(err)
		}
	}

	auditor, reader := auditor.NewMockBlessingAuditor()
	revocationManager := revocation.NewMockRevocationManager()
	oauthProvider := oauth.NewMockOAuth()

	params := blesser.OAuthBlesserParams{
		OAuthProvider:     oauthProvider,
		BlessingDuration:  duration,
		RevocationManager: revocationManager,
	}

	ctx, shutdown := v23.Init()
	defer shutdown()

	caveatSelector := caveats.NewMockCaveatSelector()
	if *browser {
		caveatSelector = caveats.NewBrowserCaveatSelector(*assetsPrefix)
	}

	listenSpec := v23.GetListenSpec(ctx)
	s := server.NewIdentityServer(
		oauthProvider,
		auditor,
		reader,
		revocationManager,
		params,
		caveatSelector,
		nil,
		*assetsPrefix,
		*mountPrefix)
	s.Serve(ctx, &listenSpec, *externalHttpAddr, *httpAddr, *tlsConfig)
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts a test version of the identityd server that
mocks out oauth, auditing, and revocation.

To generate TLS certificates so the HTTP server can use SSL:
go run $(go list -f {{.Dir}} "crypto/tls")/generate_cert.go --host <IP address>

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}
