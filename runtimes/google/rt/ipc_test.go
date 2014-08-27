package rt_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	_ "veyron/lib/testutil"
	isecurity "veyron/runtimes/google/security"
	vsecurity "veyron/security"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
)

type testService struct{}

func (*testService) EchoIDs(call ipc.ServerCall) (server, client []string) {
	return call.LocalID().Names(), call.RemoteID().Names()
}

type S []string

func newID(name string) security.PrivateID {
	id, err := isecurity.NewPrivateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

func bless(blessor security.PrivateID, blessee security.PublicID, name string) security.PublicID {
	blessedID, err := blessor.Bless(blessee, name, 5*time.Minute, nil)
	if err != nil {
		panic(err)
	}
	return blessedID
}

func add(store security.PublicIDStore, id security.PublicID, pattern security.BlessingPattern) {
	if err := store.Add(id, pattern); err != nil {
		panic(err)
	}
}

func call(r veyron2.Runtime, client ipc.Client, name string) (clientNames, serverNames []string, err error) {
	c, err := client.StartCall(r.NewContext(), name, "EchoIDs", nil)
	if err != nil {
		return nil, nil, err
	}
	if err := c.Finish(&serverNames, &clientNames); err != nil {
		return nil, nil, err
	}
	sort.Strings(clientNames)
	sort.Strings(serverNames)
	return
}

func TestClientServerIDs(t *testing.T) {
	stopServer := func(server ipc.Server) {
		if err := server.Stop(); err != nil {
			t.Fatalf("server.Stop failed: %s", err)
		}
	}
	var (
		self   = newID("self")
		google = newID("google")
		veyron = newID("veyron")

		googleGmailService   = bless(google, self.PublicID(), "gmail")
		googleYoutubeService = bless(google, self.PublicID(), "youtube")
		veyronService        = bless(veyron, self.PublicID(), "service")
		googleGmailClient    = bless(google, self.PublicID(), "gmailClient")
		googleYoutubeClient  = bless(google, self.PublicID(), "youtubeClient")
		veyronClient         = bless(veyron, self.PublicID(), "client")
	)
	isecurity.TrustIdentityProviders(google)
	isecurity.TrustIdentityProviders(veyron)

	serverR, err := rt.New(veyron2.RuntimeID(self))
	if err != nil {
		t.Fatalf("rt.New() failed: %s", err)
	}
	clientR, err := rt.New(veyron2.RuntimeID(self))
	if err != nil {
		t.Fatalf("rt.New() failed: %s", err)
	}

	// Add PublicIDs for running "google/gmail" and "google/youtube" services to
	// serverR's PublicIDStore. Since these PublicIDs are meant to be by
	// servers only they are tagged with "".
	add(serverR.PublicIDStore(), googleGmailService, "")
	add(serverR.PublicIDStore(), googleYoutubeService, "")
	// Add PublicIDs for communicating the "google/gmail" and "google/youtube" services
	// to the clientR's PublicIDStore.
	add(clientR.PublicIDStore(), googleGmailClient, "google/*")
	add(clientR.PublicIDStore(), googleYoutubeClient, "google/youtube")

	type testcase struct {
		server, client                   security.PublicID
		defaultPattern                   security.BlessingPattern
		wantServerNames, wantClientNames []string
	}
	tests := []testcase{
		{
			defaultPattern:  security.AllPrincipals,
			wantServerNames: S{"self", "google/gmail", "google/youtube"},
			wantClientNames: S{"self", "google/gmailClient", "google/youtubeClient"},
		},
		{
			defaultPattern:  "google/gmail",
			wantServerNames: S{"google/gmail"},
			wantClientNames: S{"self", "google/gmailClient"},
		},
		{
			defaultPattern:  "google/youtube",
			wantServerNames: S{"google/youtube"},
			wantClientNames: S{"self", "google/gmailClient", "google/youtubeClient"},
		},
		{
			server:          veyronService,
			defaultPattern:  security.AllPrincipals,
			wantServerNames: S{"veyron/service"},
			wantClientNames: S{"self"},
		},
		{
			client:          veyronClient,
			defaultPattern:  security.AllPrincipals,
			wantServerNames: S{"self", "google/gmail", "google/youtube"},
			wantClientNames: S{"veyron/client"},
		},
		{
			server:          veyronService,
			client:          veyronClient,
			defaultPattern:  security.AllPrincipals,
			wantServerNames: S{"veyron/service"},
			wantClientNames: S{"veyron/client"},
		},
	}
	name := func(t testcase) string {
		return fmt.Sprintf("TestCase{clientPublicIDStore: %v, serverPublicIDStore: %v, client option: %v, server option: %v}", clientR.PublicIDStore(), serverR.PublicIDStore(), t.client, t.server)
	}
	for _, test := range tests {
		if err := serverR.PublicIDStore().SetDefaultBlessingPattern(test.defaultPattern); err != nil {
			t.Errorf("serverR.PublicIDStore.SetDefaultBlessingPattern failed: %s", err)
			continue
		}
		server, err := serverR.NewServer(veyron2.LocalID(test.server))
		if err != nil {
			t.Errorf("serverR.NewServer(...) failed: %s", err)
			continue
		}
		endpoint, err := server.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Errorf("error listening to service: ", err)
			continue
		}
		defer stopServer(server)
		if err := server.Serve("", ipc.LeafDispatcher(&testService{},
			vsecurity.NewACLAuthorizer(vsecurity.NewWhitelistACL(
				map[security.BlessingPattern]security.LabelSet{
					security.AllPrincipals: security.AllLabels,
				})))); err != nil {
			t.Errorf("error serving service: ", err)
			continue
		}

		client, err := clientR.NewClient(veyron2.LocalID(test.client))
		if err != nil {
			t.Errorf("clientR.NewClient(...) failed: %s", err)
			continue
		}
		defer client.Close()

		clientNames, serverNames, err := call(clientR, client, naming.JoinAddressName(fmt.Sprintf("%v", endpoint), ""))
		if err != nil {
			t.Errorf("IPC failed: %s", err)
			continue
		}
		sort.Strings(test.wantClientNames)
		sort.Strings(test.wantServerNames)
		if !reflect.DeepEqual(clientNames, test.wantClientNames) {
			t.Errorf("TestCase: %s, Got clientNames: %v, want: %v", name(test), clientNames, test.wantClientNames)
			continue
		}
		if !reflect.DeepEqual(serverNames, test.wantServerNames) {
			t.Errorf("TestCase: %s, Got serverNames: %v, want: %v", name(test), serverNames, test.wantServerNames)
			continue
		}
	}
}
