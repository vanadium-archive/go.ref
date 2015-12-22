// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hello_test

import (
	"os"
	"testing"

	"v.io/x/ref"
	"v.io/x/ref/lib/security"
	"v.io/x/ref/lib/v23test"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/testutil"
)

func init() {
	ref.EnvClearCredentials()
}

func withCreds(dir string, c *v23test.Cmd) *v23test.Cmd {
	c.Vars[ref.EnvCredentials] = dir
	return c
}

// setupCreds makes a bunch of credentials directories.
// We do this ourselves instead of using v23test's credentials APIs because we
// want to use the actual agentd binary, so that for regression tests we can
// test against old agents.
func setupCreds(sh *v23test.Shell, names ...string) (map[string]string, error) {
	idp := testutil.NewIDProvider("root")
	out := make(map[string]string, len(names))
	for _, name := range names {
		dir := sh.MakeTempDir()
		p, err := security.CreatePersistentPrincipal(dir, nil)
		if err != nil {
			return nil, err
		}
		if err := idp.Bless(p, name); err != nil {
			return nil, err
		}
		out[name] = dir
	}
	return out, nil
}

func TestV23HelloDirect(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	creds, err := setupCreds(sh, "helloclient", "helloserver")
	if err != nil {
		t.Fatalf("Could not create credentials: %v", err)
	}
	clientbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloclient")
	serverbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloserver")

	server := withCreds(creds["helloserver"], sh.Cmd(serverbin))
	server.Start()
	name := server.S.ExpectVar("SERVER_NAME")
	if server.S.Failed() {
		t.Fatalf("Could not get SERVER_NAME: %v", server.S.Error())
	}
	withCreds(creds["helloclient"], sh.Cmd(clientbin, "--name", name)).Run()
}

func TestV23HelloAgentd(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	creds, err := setupCreds(sh, "helloclient", "helloserver")
	if err != nil {
		t.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := sh.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	serverbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	clientbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloclient")

	server := withCreds(creds["helloserver"], sh.Cmd(serverbin))
	server.Start()
	name := server.S.ExpectVar("SERVER_NAME")
	if server.S.Failed() {
		t.Fatalf("Could not get SERVER_NAME: %v", server.S.Error())
	}
	withCreds(creds["helloclient"], sh.Cmd(agentdbin, clientbin, "--name", name)).Run()
}

func TestV23HelloMounttabled(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	creds, err := setupCreds(sh, "helloclient", "helloserver", "mounttabled")
	if err != nil {
		t.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := sh.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	mounttabledbin := sh.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	serverbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	clientbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloclient")

	name := "hello"
	mounttabled := withCreds(creds["mounttabled"], sh.Cmd(agentdbin, mounttabledbin, "--v23.tcp.address", "127.0.0.1:0"))
	mounttabled.Start()
	mtname := mounttabled.S.ExpectVar("NAME")
	if mounttabled.S.Failed() {
		t.Fatalf("Could not get NAME: %v", mounttabled.S.Error())
	}
	withCreds(creds["helloserver"], sh.Cmd(agentdbin, serverbin, "--name", name, "--v23.namespace.root", mtname)).Start()
	withCreds(creds["helloclient"], sh.Cmd(agentdbin, clientbin, "--name", name, "--v23.namespace.root", mtname)).Run()
}

func TestV23HelloProxy(t *testing.T) {
	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	creds, err := setupCreds(sh, "helloclient", "helloserver", "mounttabled", "proxyd", "xproxyd")
	if err != nil {
		t.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := sh.BuildGoPkg("v.io/x/ref/services/agent/agentd")
	mounttabledbin := sh.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	xproxydbin := sh.BuildGoPkg("v.io/x/ref/services/xproxy/xproxyd")
	serverbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	clientbin := sh.BuildGoPkg("v.io/x/ref/test/hello/helloclient")

	name := "hello"
	mounttabled := withCreds(creds["mounttabled"], sh.Cmd(agentdbin, mounttabledbin, "--v23.tcp.address", "127.0.0.1:0"))
	mounttabled.Start()
	mtname := mounttabled.S.ExpectVar("NAME")
	if mounttabled.S.Failed() {
		t.Fatalf("Could not get NAME: %v", mounttabled.S.Error())
	}
	proxyname := "proxy"
	withCreds(creds["xproxyd"], sh.Cmd(agentdbin, xproxydbin, "--name", proxyname, "--v23.tcp.address", "127.0.0.1:0", "--v23.namespace.root", mtname, "--access-list", "{\"In\":[\"root\"]}")).Start()
	withCreds(creds["helloserver"], sh.Cmd(agentdbin, serverbin, "--name", name, "--v23.proxy", proxyname, "--v23.tcp.address", "", "--v23.namespace.root", mtname)).Start()
	withCreds(creds["helloclient"], sh.Cmd(agentdbin, clientbin, "--name", name, "--v23.proxy", proxyname, "--v23.tcp.address", "", "--v23.namespace.root", mtname)).Run()
}

func TestMain(m *testing.M) {
	os.Exit(v23test.Run(m.Run))
}
