// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"flag"
	"net"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/static"
	"v.io/x/ref/services/identity/internal/auditor"
	"v.io/x/ref/services/identity/internal/blesser"
	"v.io/x/ref/services/identity/internal/caveats"
	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/revocation"
	"v.io/x/ref/services/identity/internal/server"
	"v.io/x/ref/services/identity/internal/util"
)

var (
	externalHttpAddr, httpAddr, tlsConfig, assetsPrefix, mountPrefix string
	browser                                                          bool
	oauthEmail                                                       string
)

func init() {
	// Flags controlling the HTTP server
	cmdTest.Flags.StringVar(&externalHttpAddr, "external-http-addr", "", "External address on which the HTTP server listens on.  If none is provided the server will only listen on -http-addr.")
	cmdTest.Flags.StringVar(&httpAddr, "http-addr", "localhost:0", "Address on which the HTTP server listens on.")
	cmdTest.Flags.StringVar(&tlsConfig, "tls-config", "", "Comma-separated list of TLS certificate and private key files, in that order.  This must be provided.")
	cmdTest.Flags.StringVar(&assetsPrefix, "assets-prefix", "", "Host serving the web assets for the identity server.")
	cmdTest.Flags.StringVar(&mountPrefix, "mount-prefix", "identity", "Mount name prefix to use.  May be rooted.")
	cmdTest.Flags.BoolVar(&browser, "browser", false, "Whether to open a browser caveat selector.")
	cmdTest.Flags.StringVar(&oauthEmail, "oauth-email", "testemail@example.com", "Username for the mock oauth to put in the returned blessings.")
}

func main() {
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdTest)
}

var cmdTest = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runIdentityDTest),
	Name:   "identityd_test",
	Short:  "Runs HTTP server that creates security.Blessings objects",
	Long: `
Command identityd_test runs a daemon HTTP server that uses OAuth to create
security.Blessings objects.

Starts a test version of the identityd server that mocks out oauth, auditing,
and revocation.

To generate TLS certificates so the HTTP server can use SSL:
  go run $(go list -f {{.Dir}} "crypto/tls")/generate_cert.go --host <IP address>
`,
}

func runIdentityDTest(ctx *context.T, env *cmdline.Env, args []string) error {
	// Duration to use for tls cert and blessing duration.
	duration := 365 * 24 * time.Hour

	// If no tlsConfig has been provided, write and use our own.
	if flag.Lookup("tls-config").Value.String() == "" {
		addr := externalHttpAddr
		if externalHttpAddr == "" {
			addr = httpAddr
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// NOTE(caprita): The (non-test) identityd binary
			// accepts an address with no port.  Should this test
			// binary do the same instead?
			return env.UsageErrorf("Failed to parse http address %q: %v", addr, err)
		}
		certFile, keyFile, err := util.WriteCertAndKey(host, duration)
		if err != nil {
			return err
		}
		if err := flag.Set("tls-config", certFile+","+keyFile); err != nil {
			return err
		}
	}

	mockClientID := "test-client-id"
	mockClientName := "test-client"

	auditor, reader := auditor.NewMockBlessingAuditor()
	revocationManager := revocation.NewMockRevocationManager(ctx)
	oauthProvider := oauth.NewMockOAuth(oauthEmail, mockClientID)

	params := blesser.OAuthBlesserParams{
		OAuthProvider:     oauthProvider,
		BlessingDuration:  duration,
		RevocationManager: revocationManager,
		AccessTokenClients: []oauth.AccessTokenClient{
			oauth.AccessTokenClient{
				Name:     mockClientName,
				ClientID: mockClientID,
			},
		},
	}

	caveatSelector := caveats.NewMockCaveatSelector()
	if browser {
		caveatSelector = caveats.NewBrowserCaveatSelector(assetsPrefix)
	}

	listenSpec := v23.GetListenSpec(ctx)
	s := server.NewIdentityServer(
		oauthProvider,
		auditor,
		reader,
		revocationManager,
		params,
		caveatSelector,
		assetsPrefix,
		mountPrefix)
	s.Serve(ctx, &listenSpec, externalHttpAddr, httpAddr, tlsConfig)
	return nil
}
