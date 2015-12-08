// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"crypto/tls"
	"encoding/base64"
	_ "encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate .

const urlRE = "^(https://.*)$"

func httpGet(i *v23tests.T, url string) string {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	jar, err := cookiejar.New(&cookiejar.Options{})
	if err != nil {
		i.Fatalf("failed to create a cookie jar: %v", err)
	}
	client := &http.Client{
		Jar:       jar,
		Transport: transport,
	}
	resp, err := client.Get(url)
	if err != nil {
		i.Fatalf("Get(%q) failed: %v", url, err)
	}
	output, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		i.Fatalf("ReadAll() failed: %v", err)
	}
	return string(output)
}

func seekBlessings(i *v23tests.T, principal *v23tests.Binary, httpAddr string) {
	args := []string{
		"seekblessings",
		"--browser=false",
		fmt.Sprintf("--from=%s/auth/google", httpAddr),
		"-v=3",
	}
	inv := principal.Start(args...)
	// Reproduce the sleep that was present in the shell test to see if
	// thisrec allows the test to pass on macjenkins.
	// TODO(sjr): I suspect the failure is caused by race conditions
	// exacerbated by our new binary caching.
	time.Sleep(10 * time.Second)
	line := inv.ExpectSetEventuallyRE(urlRE)[0][1]
	// Scan the output of "principal seekblessings", looking for the
	// URL that can be used to retrieve the blessings.
	output := httpGet(i, line)
	if want := "Received blessings: <tt>root:u:testemail@example.com:test-extension"; !strings.Contains(output, want) {
		i.Fatalf("failed to seek blessings: %v", string(output))
	}
}

func testOauthHandler(i *v23tests.T, addr string) {
	p := testutil.NewPrincipal("foo")
	keyBytes, err := p.PublicKey().MarshalBinary()
	if err != nil {
		i.Fatal(err)
	}
	url := addr + "/auth/google/bless?token=tok&output_format=base64vom&public_key=" + base64.URLEncoding.EncodeToString(keyBytes)
	output := httpGet(i, url)
	var blessings security.Blessings
	if raw, err := base64.URLEncoding.DecodeString(output); err != nil {
		i.Fatal(err)
	} else if err = vom.Decode(raw, &blessings); err != nil {
		i.Fatal(err)
	}
	expected := "root:o:test-client-id:testemail@example.com"
	if !blessings.CouldHaveNames([]string{expected}) {
		i.Fatalf("Wanted blessing root:o:clientid:email, got %v", blessings)
	}
}

func V23TestIdentityServer(i *v23tests.T) {
	v23tests.RunRootMT(i, "--v23.tcp.address=127.0.0.1:0")
	// Start identityd:
	//
	// identityd must have credentials that recognize the root mounttable.
	// In production, the two share a common root certificate and thus
	// recognize each other. The same is done here, i.Principal()
	// wields the root key.
	identityd := i.BuildV23Pkg("v.io/x/ref/services/identity/internal/identityd_test")
	creds, err := i.Shell().NewChildCredentials("u")
	if err != nil {
		i.Fatal(err)
	}
	oauthCreds, err := i.Shell().NewChildCredentials("o")
	if err != nil {
		i.Fatal(err)
	}
	identityd = identityd.WithStartOpts(identityd.StartOpts().WithCustomCredentials(creds))
	httpAddr := identityd.Start(
		"-v23.tcp.address=127.0.0.1:0",
		"-http-addr=127.0.0.1:0",
		"-oauth-agent-path="+oauthCreds.Path()).ExpectVar("HTTP_ADDR")

	// Use the principal tool to seekblessings.
	// This tool will not run with any credentials: Its whole purpose is to "seek" them!
	principal := i.BuildGoPkg("v.io/x/ref/cmd/principal")
	// Test an initial seekblessings call.
	seekBlessings(i, principal, httpAddr)
	// Test that a subsequent call succeeds with the same
	// credentials. This means that the blessings and principal from the
	// first call works correctly.
	// TODO(ashankar): Does anyone recall what was the intent here? Running
	// the tool twice doesn't seem to help?
	seekBlessings(i, principal, httpAddr)

	testOauthHandler(i, httpAddr)
}
