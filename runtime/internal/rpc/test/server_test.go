// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type noMethodsType struct{ Field string }

type fieldType struct {
	unexported string
}
type noExportedFieldsType struct{}

func (noExportedFieldsType) F(_ *context.T, _ rpc.ServerCall, f fieldType) error { return nil }

type badObjectDispatcher struct{}

func (badObjectDispatcher) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return noMethodsType{}, nil, nil
}

// TestBadObject ensures that Serve handles bad receiver objects gracefully (in
// particular, it doesn't panic).
func TestBadObject(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	sctx := withPrincipal(t, ctx, "server")
	cctx := withPrincipal(t, ctx, "client")

	if _, _, err := v23.WithNewServer(sctx, "", nil, nil); err == nil {
		t.Fatal("should have failed")
	}
	if _, _, err := v23.WithNewServer(sctx, "", new(noMethodsType), nil); err == nil {
		t.Fatal("should have failed")
	}
	if _, _, err := v23.WithNewServer(sctx, "", new(noExportedFieldsType), nil); err == nil {
		t.Fatal("should have failed")
	}
	if _, _, err := v23.WithNewDispatchingServer(sctx, "", badObjectDispatcher{}); err != nil {
		t.Fatalf("ServeDispatcher failed: %v", err)
	}
	// TODO(mattr): It doesn't necessarily make sense to me that a bad object from
	// the dispatcher results in a retry.
	cctx, _ = context.WithTimeout(ctx, time.Second)
	var result string
	if err := v23.GetClient(cctx).Call(cctx, "servername", "SomeMethod", nil, []interface{}{&result}); err == nil {
		// TODO(caprita): Check the error type rather than
		// merely ensuring the test doesn't panic.
		t.Fatalf("Call should have failed")
	}
}

type statusServer struct{ ch chan struct{} }

func (s *statusServer) Hang(ctx *context.T, _ rpc.ServerCall) error {
	s.ch <- struct{}{} // Notify the server has received a call.
	<-s.ch             // Wait for the server to be ready to go.
	return nil
}

func TestServerStatus(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	serverChan := make(chan struct{})
	sctx, cancel := context.WithCancel(ctx)
	_, server, err := v23.WithNewServer(sctx, "test", &statusServer{serverChan}, nil)
	if err != nil {
		t.Fatal(err)
	}
	status := server.Status()
	if got, want := status.State, rpc.ServerActive; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	progress := make(chan error)
	makeCall := func(ctx *context.T) {
		call, err := v23.GetClient(ctx).StartCall(ctx, "test", "Hang", nil)
		progress <- err
		progress <- call.Finish()
	}
	go makeCall(ctx)

	// Wait for RPC to start and the server has received the call.
	if err := <-progress; err != nil {
		t.Fatal(err)
	}
	<-serverChan

	// Stop server asynchronously
	go func() {
		cancel()
		<-server.Closed()
	}()

	waitForStatus := func(want rpc.ServerState) {
		then := time.Now()
		for {
			status = server.Status()
			if got := status.State; got != want {
				if time.Now().Sub(then) > time.Minute {
					t.Fatalf("got %s, want %s", got, want)
				}
			} else {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Server should enter 'ServerStopping' state.
	waitForStatus(rpc.ServerStopping)
	// Server won't stop until the statusServer's hung method completes.
	close(serverChan)
	// Wait for RPC to finish
	if err := <-progress; err != nil {
		t.Fatal(err)
	}
	// Now that the RPC is done, the server should be able to stop.
	waitForStatus(rpc.ServerStopped)
}

func TestPublisherStatus(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	sctx := withPrincipal(t, ctx, "server")
	sctx = v23.WithListenSpec(sctx, rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{
			{"tcp", "127.0.0.1:0"},
			{"tcp", "127.0.0.1:0"},
		},
	})
	_, server, err := v23.WithNewServer(sctx, "foo", &testServer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	status := testutil.WaitForServerPublished(server)
	if got, want := len(status.PublisherStatus), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	eps := server.Status().Endpoints
	if got, want := len(eps), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}

	// Add a second name and we should now see 4 mounts, 2 for each name.
	if err := server.AddName("bar"); err != nil {
		t.Fatal(err)
	}
	status = testutil.WaitForServerPublished(server)
	if got, want := len(status.PublisherStatus), 4; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	serversPerName := map[string][]string{}
	for _, ms := range status.PublisherStatus {
		serversPerName[ms.Name] = append(serversPerName[ms.Name], ms.Server)
	}
	if got, want := len(serversPerName), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	for _, name := range []string{"foo", "bar"} {
		if got, want := len(serversPerName[name]), 2; got != want {
			t.Errorf("got %d, want %d", got, want)
		}
		sort.Strings(serversPerName[name])
		if got, want := serversPerName[name], endpointToStrings(eps); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}

func TestIsLeafServerOption(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	_, _, err := v23.WithNewDispatchingServer(ctx, "leafserver",
		&testServerDisp{&testServer{}}, options.IsLeaf(true))
	if err != nil {
		t.Fatal(err)
	}
	// we have set IsLeaf to true, sending any suffix to leafserver should result
	// in an suffix was not expected error.
	var result string
	callErr := v23.GetClient(ctx).Call(ctx, "leafserver/unwantedSuffix", "Echo", []interface{}{"Mirror on the wall"}, []interface{}{&result})
	if callErr == nil {
		t.Fatalf("Call should have failed with suffix was not expected error")
	}
}

func endpointToStrings(eps []naming.Endpoint) []string {
	r := []string{}
	for _, ep := range eps {
		r = append(r, ep.String())
	}
	sort.Strings(r)
	return r
}

type ldServer struct {
	started chan struct{}
	wait    chan struct{}
}

func (s *ldServer) Do(ctx *context.T, call rpc.ServerCall) (bool, error) {
	<-s.wait
	return ctx.Err() != nil, nil
}

func TestLameDuck(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	cases := []struct {
		timeout     time.Duration
		finishError bool
		wasCanceled bool
	}{
		{timeout: time.Minute, wasCanceled: false},
		{timeout: 0, finishError: true},
	}
	for _, c := range cases {
		s := &ldServer{wait: make(chan struct{})}
		sctx, cancel := context.WithCancel(ctx)
		_, server, err := v23.WithNewServer(sctx, "ld", s, nil, options.LameDuckTimeout(c.timeout))
		if err != nil {
			t.Fatal(err)
		}
		call, err := v23.GetClient(ctx).StartCall(ctx, "ld", "Do", nil)
		if err != nil {
			t.Fatal(err)
		}
		// Now cancel the context putting the server into lameduck mode.
		cancel()
		// Now allow the call to complete and see if the context was canceled.
		close(s.wait)

		var wasCanceled bool
		err = call.Finish(&wasCanceled)
		if c.finishError {
			if err == nil {
				t.Errorf("case: %v: Expected error for call but didn't get one", c)
			}
		} else {
			if wasCanceled != c.wasCanceled {
				t.Errorf("case %v: got %v.", c, wasCanceled)
			}
		}
		<-server.Closed()
	}
}

type dummyService struct{}

func (dummyService) Do(ctx *context.T, call rpc.ServerCall) error {
	return nil
}

func mountedBlessings(ctx *context.T, name string) ([]string, error) {
	me, err := v23.GetNamespace(ctx).Resolve(ctx, "server")
	if err != nil {
		return nil, err
	}
	for _, ms := range me.Servers {
		ep, err := naming.ParseEndpoint(ms.Server)
		if err != nil {
			return nil, err
		}
		return ep.BlessingNames(), nil
	}
	return nil, nil
}

func TestUpdateServerBlessings(t *testing.T) {
	ctx, shutdown := test.V23InitWithMounttable()
	defer shutdown()

	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	idp := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))

	serverp := testutil.NewPrincipal()
	idp.Bless(serverp, "b1")
	sctx, err := v23.WithPrincipal(ctx, serverp)
	if err != nil {
		t.Error(err)
	}

	if _, _, err = v23.WithNewServer(sctx, "server", dummyService{}, security.AllowEveryone()); err != nil {
		t.Error(err)
	}

	want := "test-blessing:b1"
	for {
		bs, err := mountedBlessings(ctx, "server")
		if err == nil && len(bs) == 1 && bs[0] == want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	pcon, err := v23.GetClient(ctx).PinConnection(ctx, "server")
	if err != nil {
		t.Error(err)
	}
	defer pcon.Unpin()
	conn := pcon.Conn()
	names, _ := security.RemoteBlessingNames(ctx, security.NewCall(&security.CallParams{
		LocalPrincipal:  v23.GetPrincipal(ctx),
		RemoteBlessings: conn.RemoteBlessings(),
	}))
	if len(names) != 1 || names[0] != want {
		t.Errorf("got %v, wanted %q", names, want)
	}

	// Now we bless the server with a new blessing (which will change its default
	// blessing).  Then we wait for the new value to propogate to the remote end.
	idp.Bless(serverp, "b2")
	want = "test-blessing:b2"
	for {
		bs, err := mountedBlessings(ctx, "server")
		if err == nil && len(bs) == 1 && bs[0] == want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		names, _ := security.RemoteBlessingNames(ctx, security.NewCall(&security.CallParams{
			LocalPrincipal:  v23.GetPrincipal(ctx),
			RemoteBlessings: conn.RemoteBlessings(),
		}))
		if len(names) == 1 || names[0] == want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
