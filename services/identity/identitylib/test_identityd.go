// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package identitylib implements a test identityd service under the
// v.io/x/ref/test/modules framework.
package identitylib

import (
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"v.io/v23"

	"v.io/x/ref/services/identity/internal/auditor"
	"v.io/x/ref/services/identity/internal/blesser"
	"v.io/x/ref/services/identity/internal/caveats"
	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/revocation"
	"v.io/x/ref/services/identity/internal/server"
	"v.io/x/ref/services/identity/internal/util"
	"v.io/x/ref/test/modules"
)

var (
	externalHttpAddr = flag.String("external-http-addr", "", "External address on which the HTTP server listens on. If none is provided the server will only listen on -http-addr.")
	httpAddr         = flag.CommandLine.String("http-addr", "localhost:0", "Address on which the HTTP server listens on.")
	tlsConfig        = flag.CommandLine.String("tls-config", "", "Comma-separated list of TLS certificate and private key files. This must be provided.")
)

const (
	TestIdentitydCommand = "test_identityd"
)

func init() {
	modules.RegisterChild(TestIdentitydCommand, modules.Usage(flag.CommandLine), startTestIdentityd)
}

func startTestIdentityd(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	// Duration to use for tls cert and blessing duration.
	duration := 365 * 24 * time.Hour

	ctx, shutdown := v23.Init()
	defer shutdown()

	// If no tls-config has been provided, generate new cert and key and use them.
	if flag.CommandLine.Lookup("tls-config").Value.String() == "" {
		addr := *externalHttpAddr
		if *externalHttpAddr == "" {
			addr = *httpAddr
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("Failed to parse %q: %v", addr, err)
		}
		certFile, keyFile, err := util.WriteCertAndKey(host, duration)
		if err != nil {
			return fmt.Errorf("Could not write cert and key: %v", err)
		}
		if err := flag.CommandLine.Set("tls-config", certFile+","+keyFile); err != nil {
			return fmt.Errorf("Could not set tls-config: %v", err)
		}
	}

	// Pick a free port if http-addr flag is not set.
	// We can't use :0 here, because the identity server calls
	// http.ListenAndServeTLS, which blocks, leaving us with no way to tell
	// what port the server is running on.  Hence, we must pass in an
	// actual port so we know where the server is running.
	if flag.CommandLine.Lookup("http-addr").Value.String() == flag.CommandLine.Lookup("http-addr").DefValue {
		if err := flag.CommandLine.Set("http-addr", "localhost:"+freePort()); err != nil {
			return fmt.Errorf("Could not set http-addr: %v", err)
		}
	}

	auditor, reader := auditor.NewMockBlessingAuditor()
	revocationManager := revocation.NewMockRevocationManager()
	oauthProvider := oauth.NewMockOAuth("testemail@example.com")

	params := blesser.OAuthBlesserParams{
		OAuthProvider:     oauthProvider,
		BlessingDuration:  duration,
		RevocationManager: revocationManager,
	}

	s := server.NewIdentityServer(
		oauthProvider,
		auditor,
		reader,
		revocationManager,
		params,
		caveats.NewMockCaveatSelector(),
		"",
		"identity")

	l := v23.GetListenSpec(ctx)

	_, eps, externalHttpAddress := s.Listen(ctx, &l, *externalHttpAddr, *httpAddr, *tlsConfig)

	fmt.Fprintf(stdout, "TEST_IDENTITYD_NAME=%s\n", eps[0])
	fmt.Fprintf(stdout, "TEST_IDENTITYD_HTTP_ADDR=%s\n", externalHttpAddress)

	modules.WaitForEOF(stdin)
	return nil
}

func freePort() string {
	l, _ := net.Listen("tcp", ":0")
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}
