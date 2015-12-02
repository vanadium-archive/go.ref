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
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/test"
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

func TestMountStatus(t *testing.T) {
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
	status := server.Status()
	eps := server.Status().Endpoints
	if got, want := len(eps), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	setLeafEndpoints(eps)
	if got, want := len(status.Mounts), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	servers := status.Mounts.Servers()
	if got, want := len(servers), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := servers, endpointToStrings(eps); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Add a second name and we should now see 4 mounts, 2 for each name.
	if err := server.AddName("bar"); err != nil {
		t.Fatal(err)
	}
	status = server.Status()
	if got, want := len(status.Mounts), 4; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	servers = status.Mounts.Servers()
	if got, want := len(servers), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := servers, endpointToStrings(eps); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	names := status.Mounts.Names()
	if got, want := len(names), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	serversPerName := map[string][]string{}
	for _, ms := range status.Mounts {
		serversPerName[ms.Name] = append(serversPerName[ms.Name], ms.Server)
	}
	if got, want := len(serversPerName), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	for _, name := range []string{"foo", "bar"} {
		if got, want := len(serversPerName[name]), 2; got != want {
			t.Fatalf("got %d, want %d", got, want)
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

func setLeafEndpoints(eps []naming.Endpoint) {
	for i := range eps {
		eps[i].(*inaming.Endpoint).IsLeaf = true
	}
}
