package rt_test

import (
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"

	_ "veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
)

type testService struct{}

func (testService) EchoBlessings(call ipc.ServerCall) []string {
	return call.RemoteBlessings().ForContext(call)
}

func newRT() veyron2.Runtime {
	r, err := rt.New(veyron2.ForceNewSecurityModel{})
	if err != nil {
		panic(err)
	}
	return r
}

type rootPrincipal struct {
	p security.Principal
	b security.Blessings
}

func newRootPrincipal(name string) *rootPrincipal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return &rootPrincipal{p, b}
}

func (r *rootPrincipal) Bless(p security.Principal, extension string) security.Blessings {
	b, err := r.p.Bless(p.PublicKey(), r.b, extension, security.UnconstrainedUse())
	if err != nil {
		panic(err)
	}
	return b
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

func TestClientServerBlessings(t *testing.T) {
	var (
		rootAlpha, rootBeta, rootUnrecognized = newRootPrincipal("alpha"), newRootPrincipal("beta"), newRootPrincipal("unrecognized")
		clientRT, serverRT                    = newRT(), newRT()
		pclient, pserver                      = clientRT.Principal(), serverRT.Principal()

		// A bunch of blessings
		alphaClient        = rootAlpha.Bless(pclient, "client")
		betaClient         = rootBeta.Bless(pclient, "client")
		unrecognizedClient = rootUnrecognized.Bless(pclient, "client")

		alphaServer        = rootAlpha.Bless(pserver, "server")
		betaServer         = rootBeta.Bless(pserver, "server")
		unrecognizedServer = rootUnrecognized.Bless(pserver, "server")
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
		for _, root := range []*rootPrincipal{rootAlpha, rootBeta} {
			if err := rt.Principal().AddToRoots(root.b); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Start the server process.
	server, err := serverRT.NewServer()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	var serverObjectName string
	if endpoint, err := server.ListenX(profiles.LocalListenSpec); err != nil {
		t.Fatal(err)
	} else {
		serverObjectName = naming.JoinAddressName(endpoint.String(), "")
	}
	if err := server.Serve("", ipc.LeafDispatcher(testService{}, vsecurity.NewACLAuthorizer(vsecurity.OpenACL()))); err != nil {
		t.Fatal(err)
	}
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
