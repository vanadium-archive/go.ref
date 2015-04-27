// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hello_test

import (
	"fmt"

	"v.io/x/ref/envvar"
	"v.io/x/ref/lib/security"
	_ "v.io/x/ref/profiles"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

// setupCredentials makes a bunch of credentials directories.
// Note that I do this myself instead of allowing the test framework
// to do it because I really want to use the agentd binary, not
// the agent that is locally hosted inside v23Tests.T.
// This is important for regression tests where we want to test against
// old agent binaries.
func setupCredentials(i *v23tests.T, names ...string) (map[string]string, error) {
	idp := testutil.NewIDProvider("root")
	out := make(map[string]string, len(names))
	for _, name := range names {
		dir := i.NewTempDir("")
		p, err := security.CreatePersistentPrincipal(dir, nil)
		if err != nil {
			return nil, err
		}
		if err := idp.Bless(p, name); err != nil {
			return nil, err
		}
		out[name] = fmt.Sprintf("%s=%s", envvar.Credentials, dir)
	}
	return out, nil
}

func V23TestHelloDirect(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	clientbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloclient")
	serverbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloserver")
	server := serverbin.WithEnv(creds["helloserver"]).Start()
	name := server.ExpectVar("SERVER_NAME")
	clientbin.WithEnv(creds["helloclient"]).Run("--name", name)
}

func V23TestHelloAgentd(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	serverbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloserver")
	clientbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloclient")
	server := agentdbin.WithEnv(creds["helloserver"]).Start(serverbin.Path())
	name := server.ExpectVar("SERVER_NAME")
	agentdbin.WithEnv(creds["helloclient"]).Run(clientbin.Path(), "--name", name)
}

func V23TestHelloMounttabled(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver", "mounttabled")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	mounttabledbin := i.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	serverbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloserver")
	clientbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloclient")
	name := "hello"
	mounttabled := agentdbin.WithEnv(creds["mounttabled"]).Start(mounttabledbin.Path(),
		"--v23.tcp.address", "127.0.0.1:0")
	mt := fmt.Sprintf("%s=%s", envvar.NamespacePrefix, mounttabled.ExpectVar("NAME"))
	agentdbin.WithEnv(creds["helloserver"], mt).Start(serverbin.Path(), "--name", name)
	agentdbin.WithEnv(creds["helloclient"], mt).Run(clientbin.Path(), "--name", name)
}

func V23TestHelloProxy(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver",
		"mounttabled", "proxyd")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	mounttabledbin := i.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	proxydbin := i.BuildGoPkg("v.io/x/ref/services/proxy/proxyd")
	serverbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloserver")
	clientbin := i.BuildGoPkg("v.io/x/ref/examples/hello/helloclient")
	proxyname := "proxy"
	name := "hello"
	mounttabled := agentdbin.WithEnv(creds["mounttabled"]).Start(mounttabledbin.Path(),
		"--v23.tcp.address", "127.0.0.1:0")
	mt := fmt.Sprintf("%s=%s", envvar.NamespacePrefix, mounttabled.ExpectVar("NAME"))
	agentdbin.WithEnv(creds["proxyd"], mt).Start(proxydbin.Path(),
		"--name", proxyname, "--v23.tcp.address", "127.0.0.1:0",
		"--access-list", "{\"In\":[\"root/*\"]}")
	server := agentdbin.WithEnv(creds["helloserver"], mt).Start(serverbin.Path(),
		"--name", name, "--v23.proxy", proxyname, "--v23.tcp.address", "")
	// Prove that we're listening on a proxy.
	if sn := server.ExpectVar("SERVER_NAME"); sn != "proxy" {
		i.Fatalf("helloserver not listening through proxy: %s.", sn)
	}
	agentdbin.WithEnv(creds["helloclient"], mt).Run(clientbin.Path(), "--name", name)
}
