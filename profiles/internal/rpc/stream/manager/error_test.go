// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager_test

import (
	"net"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"

	_ "v.io/x/ref/profiles"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/manager"
	"v.io/x/ref/profiles/internal/rpc/stream/message"
	"v.io/x/ref/profiles/internal/testing/mocks/mocknet"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func TestListenErrors(t *testing.T) {
	server := manager.InternalNew(naming.FixedRoutingID(0x1))
	pserver := testutil.NewPrincipal("server")

	// principal, no blessings
	_, _, err := server.Listen("tcp", "127.0.0.1:0", pserver, security.Blessings{}, nil)
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// blessings, no principal
	_, _, err = server.Listen("tcp", "127.0.0.1:0", nil, pserver.BlessingStore().Default(), nil)
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// bad protocol
	_, _, err = server.Listen("foo", "127.0.0.1:0", pserver, pserver.BlessingStore().Default())
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// bad address
	_, _, err = server.Listen("tcp", "xx.0.0.1:0", pserver, pserver.BlessingStore().Default())
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// bad address for proxy
	_, _, err = server.Listen("v23", "127x.0.0.1", pserver, pserver.BlessingStore().Default())
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)
}

func acceptLoop(ln stream.Listener) {
	for {
		f, err := ln.Accept()
		if err != nil {
			return
		}
		f.Close()
	}

}
func dropDataDialer(network, address string, timeout time.Duration) (net.Conn, error) {
	matcher := func(read bool, msg message.T) bool {
		switch msg.(type) {
		case *message.HopSetup:
			return true
		}
		return false
	}
	opts := mocknet.Opts{
		Mode:              mocknet.V23CloseAtMessage,
		V23MessageMatcher: matcher,
	}
	return mocknet.DialerWithOpts(opts, network, address, timeout)
}

func TestDialErrors(t *testing.T) {
	_, shutdown := test.InitForTest()
	defer shutdown()
	server := manager.InternalNew(naming.FixedRoutingID(0x55555555))
	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccc))
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")

	// bad protocol
	ep, _ := inaming.NewEndpoint(naming.FormatEndpoint("x", "127.0.0.1:2"))
	_, err := client.Dial(ep, pclient)
	if verror.ErrorID(err) != stream.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	// no server
	ep, _ = inaming.NewEndpoint(naming.FormatEndpoint("tcp", "127.0.0.1:2"))
	_, err = client.Dial(ep, pclient)
	if verror.ErrorID(err) != stream.ErrNetwork.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)

	rpc.RegisterProtocol("dropData", dropDataDialer, net.Listen)

	ln, sep, err := server.Listen("tcp", "127.0.0.1:0", pserver, pserver.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}

	// Server will just listen for flows and close them.
	go acceptLoop(ln)

	cep, err := mocknet.RewriteEndpointProtocol(sep.String(), "dropData")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Dial(cep, pclient)
	if verror.ErrorID(err) != stream.ErrNetwork.ID {
		t.Fatalf("wrong error: %s", err)
	}
	t.Log(err)
}
