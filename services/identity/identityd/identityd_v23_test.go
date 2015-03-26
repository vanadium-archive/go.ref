// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate .

const urlRE = "^(https://.*)$"

func seekBlessings(i *v23tests.T, principal *v23tests.Binary, httpaddr string) {
	args := []string{
		"seekblessings",
		"--browser=false",
		fmt.Sprintf("--from=%s/auth/google", httpaddr),
		"-v=3",
	}
	inv := principal.Start(args...)
	// Reproduce the sleep that was present in the shell test to see if
	// this allows the test to pass on macjenkins.
	// TODO(sjr): I suspect the failure is caused by race conditions
	// exacerbated by our new binary caching.
	time.Sleep(10 * time.Second)
	line := inv.ExpectSetEventuallyRE(urlRE)[0][1]
	// Scan the output of "principal seekblessings", looking for the
	// URL that can be used to retrieve the blessings.
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
	resp, err := client.Get(line)
	if err != nil {
		i.Fatalf("Get(%q) failed: %v", line, err)
	}
	output, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		i.Fatalf("ReadAll() failed: %v", err)
	}
	if want := "Received blessings"; !strings.Contains(string(output), want) {
		i.Fatalf("failed to seek blessings: %v", string(output))
	}
}

func V23TestIdentityServer(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")
	// Start identityd:
	//
	// identityd must have credentials that recognize the root mounttable.
	// In production, the two share a common root certificate and thus
	// recognize each other. The same is done here, i.Principal()
	// wields the root key.
	identityd := i.BuildV23Pkg("v.io/x/ref/services/identity/identityd_test")
	creds, err := i.Shell().NewChildCredentials("identityd")
	if err != nil {
		i.Fatal(err)
	}
	identityd = identityd.WithStartOpts(identityd.StartOpts().WithCustomCredentials(creds))
	httpaddr := identityd.Start(
		"-veyron.tcp.address=127.0.0.1:0",
		"-httpaddr=127.0.0.1:0").ExpectVar("HTTP_ADDR")

	// Use the principal tool to seekblessings.
	// This tool will not run with any credentials: Its whole purpose is to "seek" them!
	principal := i.BuildGoPkg("v.io/x/ref/cmd/principal")
	// Test an initial seekblessings call.
	seekBlessings(i, principal, httpaddr)
	// Test that a subsequent call succeeds with the same
	// credentials. This means that the blessings and principal from the
	// first call works correctly.
	// TODO(ashankar): Does anyone recall what was the intent here? Running
	// the tool twice doesn't seem to help?
	seekBlessings(i, principal, httpaddr)
}
