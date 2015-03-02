package rt_test

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"

	"v.io/v23/verror"
	"v.io/x/ref/lib/testutil"
	tsecurity "v.io/x/ref/lib/testutil/security"
	_ "v.io/x/ref/profiles"
)

//go:generate v23 test generate

type testService struct{}

func (testService) EchoBlessings(call ipc.ServerCall) ([]string, error) {
	b, _ := call.RemoteBlessings().ForCall(call)
	return b, nil
}

func (testService) Foo(ipc.ServerCall) error {
	return nil
}

type dischargeService struct {
	called int
	mu     sync.Mutex
}

func (ds *dischargeService) Discharge(call ipc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.WireDischarge, error) {
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return nil, fmt.Errorf("discharger: not a third party caveat (%v)", cav)
	}
	if err := tp.Dischargeable(call); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", tp, err)
	}
	// If its the first time being called, add an expiry caveat and a MethodCaveat for "EchoBlessings".
	// Otherwise, just add a MethodCaveat for "Foo".
	ds.mu.Lock()
	called := ds.called
	ds.mu.Unlock()
	caveats := []security.Caveat{mkCaveat(security.MethodCaveat("Foo"))}
	if called == 0 {
		caveats = []security.Caveat{
			mkCaveat(security.MethodCaveat("EchoBlessings")),
			mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Second))),
		}
	}

	d, err := call.LocalPrincipal().MintDischarge(cav, caveats[0], caveats[1:]...)
	if err != nil {
		return nil, err
	}
	return security.MarshalDischarge(d), nil
}

func newCtx(rootCtx *context.T) *context.T {
	ctx, err := v23.SetPrincipal(rootCtx, tsecurity.NewPrincipal("defaultBlessings"))
	if err != nil {
		panic(err)
	}
	return ctx
}

func union(blessings ...security.Blessings) security.Blessings {
	var ret security.Blessings
	var err error
	for _, b := range blessings {
		if ret, err = security.UnionOfBlessings(ret, b); err != nil {
			panic(err)
		}
	}
	return ret
}

func mkCaveat(cav security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return cav
}

func mkBlessings(blessings security.Blessings, err error) security.Blessings {
	if err != nil {
		panic(err)
	}
	return blessings
}

func mkThirdPartyCaveat(discharger security.PublicKey, location string, caveats ...security.Caveat) security.Caveat {
	if len(caveats) == 0 {
		caveats = []security.Caveat{security.UnconstrainedUse()}
	}
	tpc, err := security.NewPublicKeyCaveat(discharger, location, security.ThirdPartyRequirements{}, caveats[0], caveats[1:]...)
	if err != nil {
		panic(err)
	}
	return tpc
}

func startServer(ctx *context.T, s interface{}) (ipc.Server, string, error) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		return nil, "", err
	}
	endpoints, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		return nil, "", err
	}
	serverObjectName := naming.JoinAddressName(endpoints[0].String(), "")
	if err := server.Serve("", s, allowEveryone{}); err != nil {
		return nil, "", err
	}
	return server, serverObjectName, nil
}

func TestClientServerBlessings(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	var (
		rootAlpha, rootBeta, rootUnrecognized = tsecurity.NewIDProvider("alpha"), tsecurity.NewIDProvider("beta"), tsecurity.NewIDProvider("unrecognized")
		clientCtx, serverCtx                  = newCtx(ctx), newCtx(ctx)
		pclient                               = v23.GetPrincipal(clientCtx)
		pserver                               = v23.GetPrincipal(serverCtx)

		// A bunch of blessings
		alphaClient        = mkBlessings(rootAlpha.NewBlessings(pclient, "client"))
		betaClient         = mkBlessings(rootBeta.NewBlessings(pclient, "client"))
		unrecognizedClient = mkBlessings(rootUnrecognized.NewBlessings(pclient, "client"))

		alphaServer        = mkBlessings(rootAlpha.NewBlessings(pserver, "server"))
		betaServer         = mkBlessings(rootBeta.NewBlessings(pserver, "server"))
		unrecognizedServer = mkBlessings(rootUnrecognized.NewBlessings(pserver, "server"))
	)
	// Setup the client's blessing store
	pclient.BlessingStore().Set(alphaClient, "alpha/server")
	pclient.BlessingStore().Set(betaClient, "beta")
	pclient.BlessingStore().Set(unrecognizedClient, security.AllPrincipals)

	tests := []struct {
		server security.Blessings // Blessings presented by the server.

		// Expected output
		wantServer []string // Client's view of the server's blessings
		wantClient []string // Server's view fo the client's blessings
	}{
		{
			server:     unrecognizedServer,
			wantServer: nil,
			wantClient: nil,
		},
		{
			server:     alphaServer,
			wantServer: []string{"alpha/server"},
			wantClient: []string{"alpha/client"},
		},
		{
			server:     union(alphaServer, betaServer),
			wantServer: []string{"alpha/server", "beta/server"},
			wantClient: []string{"alpha/client", "beta/client"},
		},
	}

	// Have the client and server both trust both the root principals.
	for _, ctx := range []*context.T{clientCtx, serverCtx} {
		for _, b := range []security.Blessings{alphaClient, betaClient} {
			p := v23.GetPrincipal(ctx)
			if err := p.AddToRoots(b); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Let it rip!
	for _, test := range tests {
		if err := pserver.BlessingStore().SetDefault(test.server); err != nil {
			t.Errorf("pserver.SetDefault(%v) failed: %v", test.server, err)
			continue
		}
		server, serverObjectName, err := startServer(serverCtx, testService{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, client, err := v23.SetNewClient(clientCtx)
		if err != nil {
			panic(err)
		}

		var gotClient []string
		if call, err := client.StartCall(ctx, serverObjectName, "EchoBlessings", nil); err != nil {
			t.Errorf("client.StartCall failed: %v", err)
		} else if err = call.Finish(&gotClient); err != nil {
			t.Errorf("call.Finish failed: %v", err)
		} else if !reflect.DeepEqual(gotClient, test.wantClient) {
			t.Errorf("%v: Got %v, want %v for client blessings", test.server, gotClient, test.wantServer)
		} else if gotServer, _ := call.RemoteBlessings(); !reflect.DeepEqual(gotServer, test.wantServer) {
			t.Errorf("%v: Got %v, want %v for server blessings", test.server, gotServer, test.wantClient)
		}

		server.Stop()
		client.Close()
	}
}

func TestServerDischarges(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	var (
		dischargerCtx, clientCtx, serverCtx = newCtx(ctx), newCtx(ctx), newCtx(ctx)
		pdischarger                         = v23.GetPrincipal(dischargerCtx)
		pclient                             = v23.GetPrincipal(clientCtx)
		pserver                             = v23.GetPrincipal(serverCtx)
		root                                = tsecurity.NewIDProvider("root")
	)

	// Setup the server's and discharger's blessing store and blessing roots, and
	// start the server and discharger.
	if err := root.Bless(pdischarger, "discharger"); err != nil {
		t.Fatal(err)
	}
	ds := &dischargeService{}
	dischargeServer, dischargeServerName, err := startServer(dischargerCtx, ds)
	if err != nil {
		t.Fatal(err)
	}
	defer dischargeServer.Stop()
	if err := root.Bless(pserver, "server", mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName)); err != nil {
		t.Fatal(err)
	}
	server, serverName, err := startServer(serverCtx, &testService{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// Setup up the client's blessing store so that it can talk to the server.
	rootClient := mkBlessings(root.NewBlessings(pclient, "client"))
	if _, err := pclient.BlessingStore().Set(security.Blessings{}, security.AllPrincipals); err != nil {
		t.Fatal(err)
	}
	if _, err := pclient.BlessingStore().Set(rootClient, "root/server"); err != nil {
		t.Fatal(err)
	}
	if err := pclient.AddToRoots(rootClient); err != nil {
		t.Fatal(err)
	}

	// Test that the client and server can communicate with the expected set of blessings
	// when server provides appropriate discharges.
	wantClient := []string{"root/client"}
	wantServer := []string{"root/server"}
	var gotClient []string
	// This opt ensures that if the Blessings do not match the pattern, StartCall will fail.
	allowedServers := options.AllowedServersPolicy{"root/server"}

	// Create a new client.
	clientCtx, client, err := v23.SetNewClient(clientCtx)
	if err != nil {
		t.Fatal(err)
	}
	makeCall := func() error {
		if call, err := client.StartCall(clientCtx, serverName, "EchoBlessings", nil, allowedServers); err != nil {
			return err
		} else if err = call.Finish(&gotClient); err != nil {
			return fmt.Errorf("call.Finish failed: %v", err)
		} else if !reflect.DeepEqual(gotClient, wantClient) {
			return fmt.Errorf("Got %v, want %v for client blessings", gotClient, wantClient)
		} else if gotServer, _ := call.RemoteBlessings(); !reflect.DeepEqual(gotServer, wantServer) {
			return fmt.Errorf("Got %v, want %v for server blessings", gotServer, wantServer)
		}
		return nil
	}

	if err := makeCall(); err != nil {
		t.Error(err)
	}
	ds.mu.Lock()
	ds.called++
	ds.mu.Unlock()
	// makeCall should eventually fail because the discharge will expire, and when it does
	// it no longer allows calls to "EchoBlessings".
	start := time.Now()
	for {
		if time.Since(start) > time.Second {
			t.Fatalf("Discharge no refreshed in 1 second")
		}
		if err := makeCall(); err == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		} else if !verror.Is(err, verror.ErrNotTrusted.ID) {
			t.Fatalf("got error %v, expected %v", err, verror.ErrNotTrusted.ID)
		}
		break
	}

	// Discharge should now be refreshed and calls to "Foo" should succeed.
	if _, err := client.StartCall(clientCtx, serverName, "Foo", nil, allowedServers); err != nil {
		t.Errorf("client.StartCall should have succeeded: %v", err)
	}

	// Test that the client fails to talk to server that does not present appropriate discharges.
	// Setup a new client so that there are no cached VCs.
	clientCtx, client, err = v23.SetNewClient(clientCtx)
	if err != nil {
		t.Fatal(err)
	}

	rootServerInvalidTPCaveat := mkBlessings(root.NewBlessings(pserver, "server", mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName, mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Second))))))
	if err := pserver.BlessingStore().SetDefault(rootServerInvalidTPCaveat); err != nil {
		t.Fatal(err)
	}
	if call, err := client.StartCall(clientCtx, serverName, "EchoBlessings", nil); verror.Is(err, verror.ErrNoAccess.ID) {
		remoteBlessings, _ := call.RemoteBlessings()
		t.Errorf("client.StartCall passed unexpectedly with remote end authenticated as: %v", remoteBlessings)
	}
}

type allowEveryone struct{}

func (allowEveryone) Authorize(security.Call) error { return nil }
