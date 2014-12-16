package ipc

import (
	"testing"

	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

type fakeAuthorizer int

func (fakeAuthorizer) Authorize(security.Context) error {
	return nil
}

type canceld struct {
	sm        stream.Manager
	ns        naming.Namespace
	name      string
	child     string
	started   chan struct{}
	cancelled chan struct{}
	stop      func() error
}

func (c *canceld) Run(ctx ipc.ServerCall) error {
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
	vlog.Info(c.name, " cancelled")
	close(c.cancelled)
	return nil
}

func makeCanceld(ns naming.Namespace, name, child string) (*canceld, error) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x111111111))
	ctx := testContext()
	s, err := InternalNewServer(ctx, sm, ns, nil)
	if err != nil {
		return nil, err
	}
	if _, err := s.Listen(listenSpec); err != nil {
		return nil, err
	}

	c := &canceld{
		sm:        sm,
		ns:        ns,
		name:      name,
		child:     child,
		started:   make(chan struct{}, 0),
		cancelled: make(chan struct{}, 0),
		stop:      s.Stop,
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

	ctx, cancel := testContext().WithCancel()
	_, err = client.StartCall(ctx, "c1", "Run", []interface{}{})
	if err != nil {
		t.Fatal("can't call: ", err)
	}

	<-c1.started
	<-c2.started

	vlog.Info("cancelling initial call")
	cancel()

	vlog.Info("waiting for children to be cancelled")
	<-c1.cancelled
	<-c2.cancelled
}
