package rpc

import (
	"testing"

	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/manager"
	tnaming "v.io/x/ref/profiles/internal/testing/mocks/naming"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
)

type fakeAuthorizer int

func (fakeAuthorizer) Authorize(*context.T) error {
	return nil
}

type canceld struct {
	sm       stream.Manager
	ns       ns.Namespace
	name     string
	child    string
	started  chan struct{}
	canceled chan struct{}
	stop     func() error
}

func (c *canceld) Run(call rpc.StreamServerCall) error {
	close(c.started)

	client, err := InternalNewClient(c.sm, c.ns)
	if err != nil {
		vlog.Error(err)
		return err
	}

	if c.child != "" {
		if _, err = client.StartCall(call.Context(), c.child, "Run", []interface{}{}); err != nil {
			vlog.Error(err)
			return err
		}
	}

	vlog.Info(c.name, " waiting for cancellation")
	<-call.Context().Done()
	vlog.Info(c.name, " canceled")
	close(c.canceled)
	return nil
}

func makeCanceld(ctx *context.T, ns ns.Namespace, name, child string) (*canceld, error) {
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

	if err := s.Serve(name, c, fakeAuthorizer(0)); err != nil {
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
		serverCtx, _     = v23.SetPrincipal(ctx, pserver)
		clientCtx, _     = v23.SetPrincipal(ctx, pclient)
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
