// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"fmt"
	"io"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"

	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

const (
	serverCmd   = "server"
	clientCmd   = "client"
	proxyName   = "proxy"    // Name which the proxy mounts itself at
	serverName  = "server"   // Name which the server mounts itself at
	responseVar = "RESPONSE" // Name of the variable used by client program to output the response
)

func init() {
	modules.RegisterChild(serverCmd, "server", runServer)
	modules.RegisterChild(clientCmd, "client", runClient)
}

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
		Start("--v23.tcp.address=127.0.0.1:0", "--name="+proxyName)
	// Start the server that only listens via the proxy
	if _, err := t.Shell().StartWithOpts(
		t.Shell().DefaultStartOpts().WithCustomCredentials(serverCreds),
		nil,
		serverCmd); err != nil {
		t.Fatal(err)
	}
	// Run the client.
	client, err := t.Shell().StartWithOpts(
		t.Shell().DefaultStartOpts().WithCustomCredentials(clientCreds),
		nil,
		clientCmd)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.ExpectVar(responseVar), "server [root/server] saw client [root/client]"; got != want {
		t.Fatalf("Got %q, want %q", got, want)
	}
}

func runServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	server, err := v23.NewServer(ctx)
	if err != nil {
		return err
	}
	defer server.Stop()
	if _, err := server.Listen(rpc.ListenSpec{Proxy: proxyName}); err != nil {
		return err
	}
	if err := server.Serve(serverName, service{}, allowEveryone{}); err != nil {
		return err
	}

	modules.WaitForEOF(stdin)
	return nil
}

func runClient(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	call, err := v23.GetClient(ctx).StartCall(ctx, serverName, "Echo", nil)
	if err != nil {
		return err
	}
	var response string
	if err := call.Finish(&response); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%v=%v\n", responseVar, response)
	return nil
}

type service struct{}

func (service) Echo(ctx *context.T, call rpc.ServerCall) (string, error) {
	client, _ := security.RemoteBlessingNames(ctx, call.Security())
	server := security.LocalBlessingNames(ctx, call.Security())
	return fmt.Sprintf("server %v saw client %v", server, client), nil
}

type allowEveryone struct{}

func (allowEveryone) Authorize(*context.T, security.Call) error { return nil }
