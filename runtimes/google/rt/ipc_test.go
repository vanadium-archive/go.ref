package rt_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/profiles"
	"v.io/core/veyron2/vdl/vdlutil"
	"v.io/core/veyron2/verror"
)

func init() { testutil.Init() }

type testService struct{}

func (testService) EchoBlessings(call ipc.ServerContext) []string {
	return call.RemoteBlessings().ForContext(call)
}

type dischargeService struct{}

func (dischargeService) Discharge(ctx ipc.ServerCall, cav vdlutil.Any, _ security.DischargeImpetus) (vdlutil.Any, error) {
	c, ok := cav.(security.ThirdPartyCaveat)
	if !ok {
		return nil, fmt.Errorf("discharger: unknown caveat(%T)", cav)
	}
	if err := c.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", c, err)
	}
	return ctx.LocalPrincipal().MintDischarge(c, security.UnconstrainedUse())
}

func newRT() veyron2.Runtime {
	r, err := rt.New()
	if err != nil {
		panic(err)
	}
	return r
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

func newCaveat(v security.CaveatValidator) security.Caveat {
	cav, err := security.NewCaveat(v)
	if err != nil {
		panic(err)
	}
	return cav
}

func mkCaveat(cav security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return cav
}

func mkThirdPartyCaveat(discharger security.PublicKey, location string, caveats ...security.Caveat) security.Caveat {
	if len(caveats) == 0 {
		caveats = []security.Caveat{security.UnconstrainedUse()}
	}
	tpc, err := security.NewPublicKeyCaveat(discharger, location, security.ThirdPartyRequirements{}, caveats[0], caveats[1:]...)
	if err != nil {
		panic(err)
	}
	return newCaveat(tpc)
}

func startServer(runtime veyron2.Runtime, s interface{}) (ipc.Server, string, error) {
	server, err := runtime.NewServer()
	if err != nil {
		return nil, "", err
	}
	endpoints, err := server.Listen(profiles.LocalListenSpec)
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
	b := func(blessings security.Blessings, err error) security.Blessings {
		if err != nil {
			t.Fatal(err)
		}
		return blessings
	}
	var (
		rootAlpha, rootBeta, rootUnrecognized = tsecurity.NewIDProvider("alpha"), tsecurity.NewIDProvider("beta"), tsecurity.NewIDProvider("unrecognized")
		clientRT, serverRT                    = newRT(), newRT()
		pclient, pserver                      = clientRT.Principal(), serverRT.Principal()

		// A bunch of blessings
		alphaClient        = b(rootAlpha.NewBlessings(pclient, "client"))
		betaClient         = b(rootBeta.NewBlessings(pclient, "client"))
		unrecognizedClient = b(rootUnrecognized.NewBlessings(pclient, "client"))

		alphaServer        = b(rootAlpha.NewBlessings(pserver, "server"))
		betaServer         = b(rootBeta.NewBlessings(pserver, "server"))
		unrecognizedServer = b(rootUnrecognized.NewBlessings(pserver, "server"))
	)
	// Setup the client's blessing store
	pclient.BlessingStore().Set(alphaClient, "alpha/server")
	pclient.BlessingStore().Set(betaClient, "beta/...")
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
	for _, rt := range []veyron2.Runtime{clientRT, serverRT} {
		for _, b := range []security.Blessings{alphaClient, betaClient} {
			if err := rt.Principal().AddToRoots(b); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Start the server process.
	server, serverObjectName, err := startServer(serverRT, testService{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// Let it rip!
	for _, test := range tests {
		// Create a new client per test so as to not re-use established authenticated VCs.
		// TODO(ashankar,suharshs): Once blessings are exchanged "per-RPC", one client for all cases will suffice.
		client, err := clientRT.NewClient()
		if err != nil {
			t.Errorf("clientRT.NewClient failed: %v", err)
			continue
		}
		if err := pserver.BlessingStore().SetDefault(test.server); err != nil {
			t.Errorf("pserver.SetDefault(%v) failed: %v", test.server, err)
			continue
		}
		var gotClient []string
		if call, err := client.StartCall(clientRT.NewContext(), serverObjectName, "EchoBlessings", nil); err != nil {
			t.Errorf("client.StartCall failed: %v", err)
		} else if err = call.Finish(&gotClient); err != nil {
			t.Errorf("call.Finish failed: %v", err)
		} else if !reflect.DeepEqual(gotClient, test.wantClient) {
			t.Errorf("%v: Got %v, want %v for client blessings", test.server, gotClient, test.wantServer)
		} else if gotServer, _ := call.RemoteBlessings(); !reflect.DeepEqual(gotServer, test.wantServer) {
			t.Errorf("%v: Got %v, want %v for server blessings", test.server, gotServer, test.wantClient)
		}
		client.Close()
	}
}

func TestServerDischarges(t *testing.T) {
	b := func(blessings security.Blessings, err error) security.Blessings {
		if err != nil {
			t.Fatal(err)
		}
		return blessings
	}
	var (
		dischargerRT, clientRT, serverRT = newRT(), newRT(), newRT()
		pdischarger, pclient, pserver    = dischargerRT.Principal(), clientRT.Principal(), serverRT.Principal()
		root                             = tsecurity.NewIDProvider("root")
	)

	// Start the server and discharge server.
	server, serverName, err := startServer(serverRT, &testService{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	dischargeServer, dischargeServerName, err := startServer(dischargerRT, &dischargeService{})
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

	// Create a new client.
	client, err := clientRT.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Setup up the client's blessing store so that it can talk to the server.
	rootClient := b(root.NewBlessings(pclient, "client"))
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
	if call, err := client.StartCall(clientRT.NewContext(), serverName, "EchoBlessings", nil); err != nil {
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
	if client, err = clientRT.NewClient(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	rootServerInvalidTPCaveat := b(root.NewBlessings(pserver, "server", mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName, mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Second))))))
	if err := pserver.BlessingStore().SetDefault(rootServerInvalidTPCaveat); err != nil {
		t.Fatal(err)
	}
	if call, err := client.StartCall(clientRT.NewContext(), serverName, "EchoBlessings", nil); verror.Is(err, verror.NoAccess) {
		remoteBlessings, _ := call.RemoteBlessings()
		t.Errorf("client.StartCall passed unexpectedly with remote end authenticated as: %v", remoteBlessings)
	}
}

type allowEveryone struct{}

func (allowEveryone) Authorize(security.Context) error { return nil }
