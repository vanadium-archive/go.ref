// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP server that uses OAuth to create security.Blessings objects.
// For more information on our setup of the identity server see:
// https://docs.google.com/document/d/1ebQ1sQn95cFu8yQM36rpJ8mQvsU29aa1o03ADhi52BM
package main

import (
	"database/sql"
	"flag"
	"fmt"
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
	// Configuration for various Google OAuth-based clients.
	googleConfigWeb     = flag.String("google_config_web", "", "Path to JSON-encoded OAuth client configuration for the web application that renders the audit log for blessings provided by this provider.")
	googleConfigChrome  = flag.String("google_config_chrome", "", "Path to the JSON-encoded OAuth client configuration for Chrome browser applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleConfigAndroid = flag.String("google_config_android", "", "Path to the JSON-encoded OAuth client configuration for Android applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	emailClassifier     util.EmailClassifier

	// Flags controlling the HTTP server
	host      = flag.String("host", defaultHost(), "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Vanadium identities will have the IssuerName set to 'x.com' and clients can expect to find the root name and public key of the signer at 'x.com/blessing-root'.")
	httpaddr  = flag.String("httpaddr", "localhost:8125", "Address on which the HTTP server listens on.")
	tlsconfig = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files, in that order. This must be provided.")
)

func main() {
	flag.Var(&emailClassifier, "email_classifier", "A comma-separated list of <domain>=<prefix> pairs. For example 'google.com=internal,v.io=trusted'. When specified, then the blessings generated for email address of <domain> will use the extension <prefix>/<email> instead of the default extension of users/<email>.")
	flag.Usage = usage
	ctx, shutdown := v23.Init()
	defer shutdown()

	var sqlDB *sql.DB
	var err error
	if len(*sqlConf) > 0 {
		if sqlDB, err = dbFromConfigFile(*sqlConf); err != nil {
			vlog.Fatalf("failed to create sqlDB: %v", err)
		}
	}

	googleoauth, err := oauth.NewGoogleOAuth(*googleConfigWeb)
	if err != nil {
		vlog.Fatalf("Failed to setup GoogleOAuth: %v", err)
	}

	auditor, reader, err := auditor.NewSQLBlessingAuditor(sqlDB)
	if err != nil {
		vlog.Fatalf("Failed to create sql auditor from config: %v", err)
	}

	revocationManager, err := revocation.NewRevocationManager(sqlDB)
	if err != nil {
		vlog.Fatalf("Failed to start RevocationManager: %v", err)
	}

	listenSpec := v23.GetListenSpec(ctx)
	s := server.NewIdentityServer(
		googleoauth,
		auditor,
		reader,
		revocationManager,
		googleOAuthBlesserParams(googleoauth, revocationManager),
		caveats.NewBrowserCaveatSelector(),
		&emailClassifier)
	s.Serve(ctx, &listenSpec, *host, *httpaddr, *tlsconfig)
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts an HTTP server that brokers blessings after authenticating through OAuth.

To generate TLS certificates so the HTTP server can use SSL:
go run $(go list -f {{.Dir}} "crypto/tls")/generate_cert.go --host <IP address>

To use Google as an OAuth provider the --google_config_* flags must be set to point to
the a JSON file obtained after registering the application with the Google Developer Console
at https://cloud.google.com/console

More details on Google OAuth at:
https://developers.google.com/accounts/docs/OAuth2Login

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}

func googleOAuthBlesserParams(oauthProvider oauth.OAuthProvider, revocationManager revocation.RevocationManager) blesser.OAuthBlesserParams {
	params := blesser.OAuthBlesserParams{
		OAuthProvider:     oauthProvider,
		BlessingDuration:  365 * 24 * time.Hour,
		EmailClassifier:   &emailClassifier,
		RevocationManager: revocationManager,
	}
	if clientID, err := getOAuthClientID(*googleConfigChrome); err != nil {
		vlog.Info(err)
	} else {
		params.AccessTokenClients = append(params.AccessTokenClients, oauth.AccessTokenClient{Name: "chrome", ClientID: clientID})
	}
	if clientID, err := getOAuthClientID(*googleConfigAndroid); err != nil {
		vlog.Info(err)
	} else {
		params.AccessTokenClients = append(params.AccessTokenClients, oauth.AccessTokenClient{Name: "android", ClientID: clientID})
	}
	return params
}

func getOAuthClientID(configFile string) (clientID string, err error) {
	f, err := os.Open(configFile)
	if err != nil {
		return "", fmt.Errorf("failed to open %q: %v", configFile, err)
	}
	defer f.Close()
	clientID, err = oauth.ClientIDFromJSON(f)
	if err != nil {
		return "", fmt.Errorf("failed to decode JSON in %q: %v", configFile, err)
	}
	return clientID, nil
}

func defaultHost() string {
	host, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname() failed: %v", err)
	}
	return host
}
