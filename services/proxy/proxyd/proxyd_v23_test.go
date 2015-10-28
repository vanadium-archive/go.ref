// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate

const (
	proxyName   = "proxy"    // Name which the proxy mounts itself at
	serverName  = "server"   // Name which the server mounts itself at
	responseVar = "RESPONSE" // Name of the variable used by client program to output the response
)

func V23TestProxyd(t *v23tests.T) {
	v23tests.RunRootMT(t, "--v23.tcp.address=127.0.0.1:0")
	var (
		proxydCreds, _ = t.Shell().NewChildCredentials("proxyd")
		serverCreds, _ = t.Shell().NewChildCredentials("server")
		clientCreds, _ = t.Shell().NewChildCredentials("client")
		proxyd         = t.BuildV23Pkg("v.io/x/ref/services/proxy/proxyd")
	)
	// Start proxyd
	proxyd.WithStartOpts(proxyd.StartOpts().WithCustomCredentials(proxydCreds)).
		Start("--v23.tcp.address=127.0.0.1:0", "--name="+proxyName, "--access-list", "{\"In\":[\"root/server\"]}")
	// Start the server that only listens via the proxy
	if _, err := t.Shell().StartWithOpts(
		t.Shell().DefaultStartOpts().WithCustomCredentials(serverCreds),
		nil,
		runServer); err != nil {
		t.Fatal(err)
	}
	// Run the client.
	client, err := t.Shell().StartWithOpts(
		t.Shell().DefaultStartOpts().WithCustomCredentials(clientCreds),
		nil,
		runClient)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.ExpectVar(responseVar), "server [root/server] saw client [root/client]"; got != want {
		t.Fatalf("Got %q, want %q", got, want)
	}
}

var runServer = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()
	// Set the listen spec to listen only via the proxy.
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: proxyName})
	if _, _, err := v23.WithNewServer(ctx, serverName, service{}, security.AllowEveryone()); err != nil {
		return err
	}
	modules.WaitForEOF(env.Stdin)
	return nil
}, "runServer")

var runClient = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()
	var response string
	if err := v23.GetClient(ctx).Call(ctx, serverName, "Echo", nil, []interface{}{&response}); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "%v=%v\n", responseVar, response)
	return nil
}, "runClient")

type service struct{}

func (service) Echo(ctx *context.T, call rpc.ServerCall) (string, error) {
	client, _ := security.RemoteBlessingNames(ctx, call.Security())
	server := security.LocalBlessingNames(ctx, call.Security())
	return fmt.Sprintf("server %v saw client %v", server, client), nil
}
