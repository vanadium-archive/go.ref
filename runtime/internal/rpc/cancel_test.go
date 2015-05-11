// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	tnaming "v.io/x/ref/runtime/internal/testing/mocks/naming"
)

type canceld struct {
	sm       stream.Manager
	ns       namespace.T
	name     string
	child    string
	started  chan struct{}
	canceled chan struct{}
	stop     func() error
}

func (c *canceld) Run(ctx *context.T, _ rpc.StreamServerCall) error {
	close(c.started)

	client, err := InternalNewClient(c.sm, c.ns)
	if err != nil {
		vlog.Error(err)
		return err
	}

	if c.child != "" {
		if _, err = client.StartCall(ctx, c.child, "Run", []interface{}{}); err != nil {
			vlog.Error(err)
			return err
		}
	}

	vlog.Info(c.name, " waiting for cancellation")
	<-ctx.Done()
	vlog.Info(c.name, " canceled")
	close(c.canceled)
	return nil
}

func makeCanceld(ctx *context.T, ns namespace.T, name, child string) (*canceld, error) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x111111111))
	s, err := testInternalNewServer(ctx, sm, ns, v23.GetPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	if _, err := s.Listen(listenSpec); err != nil {
		return nil, err
	}

	c := &canceld{
		sm:       sm,
		ns:       ns,
		name:     name,
		child:    child,
		started:  make(chan struct{}, 0),
		canceled: make(chan struct{}, 0),
		stop:     s.Stop,
	}

	if err := s.Serve(name, c, security.AllowEveryone()); err != nil {
		return nil, err
	}

	return c, nil
}

// TestCancellationPropagation tests that cancellation propogates along an
// RPC call chain without user intervention.
func TestCancellationPropagation(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		sm               = manager.InternalNew(naming.FixedRoutingID(0x555555555))
		ns               = tnaming.NewSimpleNamespace()
		pclient, pserver = newClientServerPrincipals()
		serverCtx, _     = v23.WithPrincipal(ctx, pserver)
		clientCtx, _     = v23.WithPrincipal(ctx, pclient)
	)
	client, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Error(err)
	}

	c1, err := makeCanceld(serverCtx, ns, "c1", "c2")
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c1.stop()

	c2, err := makeCanceld(serverCtx, ns, "c2", "")
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c2.stop()

	clientCtx, cancel := context.WithCancel(clientCtx)
	_, err = client.StartCall(clientCtx, "c1", "Run", []interface{}{})
	if err != nil {
		t.Fatal("can't call: ", err)
	}

	<-c1.started
	<-c2.started

	vlog.Info("cancelling initial call")
	cancel()

	vlog.Info("waiting for children to be canceled")
	<-c1.canceled
	<-c2.canceled
}
