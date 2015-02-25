package ipc

import (
	"testing"

	"v.io/core/veyron/runtimes/google/ipc/stream/manager"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"

	"v.io/core/veyron/runtimes/google/ipc/stream"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/security"
	"v.io/v23/vlog"
)

type fakeAuthorizer int

func (fakeAuthorizer) Authorize(security.Context) error {
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

func (c *canceld) Run(ctx ipc.ServerCall) error {
	close(c.started)

	client, err := InternalNewClient(c.sm, c.ns)
	if err != nil {
		vlog.Error(err)
		return err
	}

	if c.child != "" {
		if _, err = client.StartCall(ctx.Context(), c.child, "Run", []interface{}{}); err != nil {
			vlog.Error(err)
			return err
		}
	}

	vlog.Info(c.name, " waiting for cancellation")
	<-ctx.Context().Done()
	vlog.Info(c.name, " canceled")
	close(c.canceled)
	return nil
}

func makeCanceld(ns ns.Namespace, name, child string) (*canceld, error) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x111111111))
	ctx := testContext()
	s, err := testInternalNewServer(ctx, sm, ns)
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
	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()

	client, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Error(err)
	}

	c1, err := makeCanceld(ns, "c1", "c2")
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c1.stop()

	c2, err := makeCanceld(ns, "c2", "")
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c2.stop()

	ctx, cancel := context.WithCancel(testContext())
	_, err = client.StartCall(ctx, "c1", "Run", []interface{}{})
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
