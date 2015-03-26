// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

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
	externalHttpAddr = flag.String("externalhttpaddr", "", "External address on which the HTTP server listens on. If none is provided the server will only listen on -httpaddr.")
	httpAddr         = flag.CommandLine.String("httpaddr", "localhost:0", "Address on which the HTTP server listens on.")
	tlsconfig        = flag.CommandLine.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. This must be provided.")
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

	// If no tlsconfig has been provided, generate new cert and key and use them.
	if flag.CommandLine.Lookup("tlsconfig").Value.String() == "" {
		addr := *externalHttpAddr
		if *externalHttpAddr == "" {
			addr = *httpAddr
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("Failed to parse %q: %v", httpAddr, err)
		}
		certFile, keyFile, err := util.WriteCertAndKey(host, duration)
		if err != nil {
			return fmt.Errorf("Could not write cert and key: %v", err)
		}
		if err := flag.CommandLine.Set("tlsconfig", certFile+","+keyFile); err != nil {
			return fmt.Errorf("Could not set tlsconfig: %v", err)
		}
	}

	// Pick a free port if httpaddr flag is not set.
	// We can't use :0 here, because the identity server calls
	// http.ListenAndServeTLS, which blocks, leaving us with no way to tell
	// what port the server is running on.  Hence, we must pass in an
	// actual port so we know where the server is running.
	if flag.CommandLine.Lookup("httpaddr").Value.String() == flag.CommandLine.Lookup("httpaddr").DefValue {
		if err := flag.CommandLine.Set("httpaddr", "localhost:"+freePort()); err != nil {
			return fmt.Errorf("Could not set httpaddr: %v", err)
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

	s := server.NewIdentityServer(
		oauthProvider,
		auditor,
		reader,
		revocationManager,
		params,
		caveats.NewMockCaveatSelector(),
		nil,
		"",
		"identity")

	l := v23.GetListenSpec(ctx)

	_, eps, externalHttpAddress := s.Listen(ctx, &l, *externalHttpAddr, *httpAddr, *tlsconfig)

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
