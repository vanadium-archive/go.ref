package main_test

import (
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"v.io/core/veyron/lib/testutil/v23tests"
)

//go:generate v23 test generate .

var (
	urlRE = "^(https://.*)$"
)

func seekBlessings(i *v23tests.T, principal *v23tests.Binary) {
	args := []string{
		"seekblessings",
		"--browser=false",
		"--from=https://localhost:8125/google",
		"-v=3",
	}
	inv := principal.Start(args...)
	// Reproduce the sleep that was present in the shell test to see if
	// this allows the test to pass on macjenkins.
	time.Sleep(2 * time.Second)
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

	// Start the identity server.
	args := []string{
		"-host=localhost",
		"-veyron.tcp.address=127.0.0.1:0",
	}
	i.BuildGoPkg("v.io/core/veyron/services/identity/identityd_test").Start(args...)

	// Use the principal tool to seekblessings.
	principal := i.BuildGoPkg("v.io/core/veyron/tools/principal")
	// Test an initial seekblessings call.
	seekBlessings(i, principal)
	// Test that a subsequent call succeeds with the same
	// credentials. This means that the blessings and principal from the
	// first call works correctly.
	seekBlessings(i, principal)
}
