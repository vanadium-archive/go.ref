// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hello_test

import (
	"fmt"
	"os"
	"time"

	"v.io/x/ref"
	"v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate

func init() {
	ref.EnvClearCredentials()
}

var opts = modules.StartOpts{
	StartTimeout:    20 * time.Second,
	ShutdownTimeout: 20 * time.Second,
	ExpectTimeout:   20 * time.Second,
	ExecProtocol:    false,
	External:        true,
}

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
		out[name] = fmt.Sprintf("%s=%s", ref.EnvCredentials, dir)
	}
	return out, nil
}

func V23TestHelloDirect(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	clientbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloclient")
	serverbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	server := serverbin.WithStartOpts(opts).WithEnv(creds["helloserver"]).Start()
	name := server.ExpectVar("SERVER_NAME")
	if server.Failed() {
		server.Wait(os.Stdout, os.Stderr)
		i.Fatalf("Could not get SERVER_NAME: %v", server.Error())
	}
	clientbin.WithEnv(creds["helloclient"]).WithStartOpts(opts).Run("--name", name)
}

func V23TestHelloAgentd(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd").WithStartOpts(opts)
	serverbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	clientbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloclient")
	server := agentdbin.WithEnv(creds["helloserver"]).Start(serverbin.Path())
	name := server.ExpectVar("SERVER_NAME")
	if server.Failed() {
		server.Wait(os.Stdout, os.Stderr)
		i.Fatalf("Could not get SERVER_NAME: %v", server.Error())
	}
	agentdbin.WithEnv(creds["helloclient"]).Run(clientbin.Path(), "--name", name)
}

func V23TestHelloMounttabled(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver", "mounttabled")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd").WithStartOpts(opts)
	mounttabledbin := i.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	serverbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	clientbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloclient")
	name := "hello"
	mounttabled := agentdbin.WithEnv(creds["mounttabled"]).Start(mounttabledbin.Path(),
		"--v23.tcp.address", "127.0.0.1:0")
	mtname := mounttabled.ExpectVar("NAME")
	if mounttabled.Failed() {
		mounttabled.Wait(os.Stdout, os.Stderr)
		i.Fatalf("Could not get NAME: %v", mounttabled.Error())
	}
	agentdbin.WithEnv(creds["helloserver"]).Start(serverbin.Path(), "--name", name,
		"--v23.namespace.root", mtname)
	agentdbin.WithEnv(creds["helloclient"]).Run(clientbin.Path(), "--name", name,
		"--v23.namespace.root", mtname)
}

func V23TestHelloProxy(i *v23tests.T) {
	creds, err := setupCredentials(i, "helloclient", "helloserver",
		"mounttabled", "proxyd", "xproxyd")
	if err != nil {
		i.Fatalf("Could not create credentials: %v", err)
	}
	agentdbin := i.BuildGoPkg("v.io/x/ref/services/agent/agentd").WithStartOpts(opts)
	mounttabledbin := i.BuildGoPkg("v.io/x/ref/services/mounttable/mounttabled")
	xproxydbin := i.BuildGoPkg("v.io/x/ref/services/xproxy/xproxyd")
	proxydbin := i.BuildGoPkg("v.io/x/ref/services/proxy/proxyd")
	serverbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloserver")
	clientbin := i.BuildGoPkg("v.io/x/ref/test/hello/helloclient")
	proxyname := "proxy"
	name := "hello"
	mounttabled := agentdbin.WithEnv(creds["mounttabled"]).Start(mounttabledbin.Path(),
		"--v23.tcp.address", "127.0.0.1:0")
	mtname := mounttabled.ExpectVar("NAME")
	if mounttabled.Failed() {
		mounttabled.Wait(os.Stdout, os.Stderr)
		i.Fatalf("Could not get NAME: %v", mounttabled.Error())
	}
	agentdbin.WithEnv(creds["proxyd"]).Start(proxydbin.Path(),
		"--name", proxyname, "--v23.tcp.address", "127.0.0.1:0",
		"--v23.namespace.root", mtname,
		"--access-list", "{\"In\":[\"root\"]}")
	agentdbin.WithEnv(creds["xproxyd"]).Start(xproxydbin.Path(),
		"--name", proxyname, "--v23.tcp.address", "127.0.0.1:0",
		"--v23.namespace.root", mtname,
		"--access-list", "{\"In\":[\"root\"]}")
	agentdbin.WithEnv(creds["helloserver"]).Start(serverbin.Path(),
		"--name", name, "--v23.proxy", proxyname, "--v23.tcp.address", "",
		"--v23.namespace.root", mtname)
	agentdbin.WithEnv(creds["helloclient"]).Run(clientbin.Path(), "--name", name,
		"--v23.namespace.root", mtname)
}
