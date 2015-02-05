package rt_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/verror"
)

func init() { testutil.Init() }

type testService struct{}

func (testService) EchoBlessings(call ipc.ServerContext) []string {
	return call.RemoteBlessings().ForContext(call)
}

type dischargeService struct{}

func (dischargeService) Discharge(ctx ipc.ServerCall, cav vdl.AnyRep, _ security.DischargeImpetus) (vdl.AnyRep, error) {
	c, ok := cav.(security.ThirdPartyCaveat)
	if !ok {
		return nil, fmt.Errorf("discharger: unknown caveat(%T)", cav)
	}
	if err := c.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", c, err)
	}
	return ctx.LocalPrincipal().MintDischarge(c, security.UnconstrainedUse())
}

func newCtx(rootCtx *context.T) *context.T {
	ctx, err := veyron2.SetPrincipal(rootCtx, tsecurity.NewPrincipal("defaultBlessings"))
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
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		return nil, "", err
	}
	endpoints, err := server.Listen(veyron2.GetListenSpec(ctx))
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
		pclient                               = veyron2.GetPrincipal(clientCtx)
		pserver                               = veyron2.GetPrincipal(serverCtx)

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
			p := veyron2.GetPrincipal(ctx)
			if err := p.AddToRoots(b); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Start the server process.
	server, serverObjectName, err := startServer(serverCtx, testService{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// Let it rip!
	for _, test := range tests {
		// TODO(ashankar,suharshs): Once blessings are exchanged "per-RPC", one client for all cases will suffice.
		// Also, we need server to lameduck VCs when server.BlessingStore().Default() has changed
		// for one client to be sufficient.
		ctx, client, err := veyron2.SetNewClient(clientCtx)
		if err != nil {
			panic(err)
		}
		if err := pserver.BlessingStore().SetDefault(test.server); err != nil {
			t.Errorf("pserver.SetDefault(%v) failed: %v", test.server, err)
			continue
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
	}
}

func TestServerDischarges(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	var (
		dischargerCtx, clientCtx, serverCtx = newCtx(ctx), newCtx(ctx), newCtx(ctx)
		pdischarger                         = veyron2.GetPrincipal(dischargerCtx)
		pclient                             = veyron2.GetPrincipal(clientCtx)
		pserver                             = veyron2.GetPrincipal(serverCtx)
		root                                = tsecurity.NewIDProvider("root")
	)

	// Start the server and discharge server.
	server, serverName, err := startServer(serverCtx, &testService{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	dischargeServer, dischargeServerName, err := startServer(dischargerCtx, &dischargeService{})
	if err != nil {
		t.Fatal(err)
	}
	defer dischargeServer.Stop()

	// Setup the server's and discharger's blessing store and blessing roots.
	if err := root.Bless(pserver, "server", mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName)); err != nil {
		t.Fatal(err)
	}
	if err := root.Bless(pdischarger, "discharger"); err != nil {
		t.Fatal(err)
	}

	// Setup up the client's blessing store so that it can talk to the server.
	rootClient := mkBlessings(root.NewBlessings(pclient, "client"))
	if _, err := pclient.BlessingStore().Set(nil, security.AllPrincipals); err != nil {
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

	// Create a new client.
	clientCtx, client, err := veyron2.SetNewClient(clientCtx)
	if err != nil {
		t.Fatal(err)
	}
	if call, err := client.StartCall(clientCtx, serverName, "EchoBlessings", nil); err != nil {
		t.Errorf("client.StartCall failed: %v", err)
	} else if err = call.Finish(&gotClient); err != nil {
		t.Errorf("call.Finish failed: %v", err)
	} else if !reflect.DeepEqual(gotClient, wantClient) {
		t.Errorf("Got %v, want %v for client blessings", gotClient, wantClient)
	} else if gotServer, _ := call.RemoteBlessings(); !reflect.DeepEqual(gotServer, wantServer) {
		t.Errorf("Got %v, want %v for server blessings", gotServer, wantServer)
	}

	// Test that the client fails to talk to server that does not present appropriate discharges.
	// Setup a new client so that there are no cached VCs.
	clientCtx, client, err = veyron2.SetNewClient(clientCtx)
	if err != nil {
		t.Fatal(err)
	}

	rootServerInvalidTPCaveat := mkBlessings(root.NewBlessings(pserver, "server", mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName, mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Second))))))
	if err := pserver.BlessingStore().SetDefault(rootServerInvalidTPCaveat); err != nil {
		t.Fatal(err)
	}
	if call, err := client.StartCall(clientCtx, serverName, "EchoBlessings", nil); verror.Is(err, verror.NoAccess) {
		remoteBlessings, _ := call.RemoteBlessings()
		t.Errorf("client.StartCall passed unexpectedly with remote end authenticated as: %v", remoteBlessings)
	}
}

type allowEveryone struct{}

func (allowEveryone) Authorize(security.Context) error { return nil }
