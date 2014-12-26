package mounttable

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/profiles"
)

func init() { testutil.Init() }

func protocolAndAddress(e naming.Endpoint) (string, string, error) {
	addr := e.Addr()
	if addr == nil {
		return "", "", fmt.Errorf("failed to get address")
	}
	return addr.Network(), addr.String(), nil
}

func TestNeighborhood(t *testing.T) {
	vlog.Infof("TestNeighborhood")
	server, err := rootRT.NewServer()
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	defer server.Stop()

	// Start serving on a loopback address.
	eps, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	estr := eps[0].String()
	addresses := []string{
		naming.JoinAddressName(estr, ""),
		naming.JoinAddressName(estr, "suffix1"),
		naming.JoinAddressName(estr, "suffix2"),
	}

	// Create a name for the server.
	serverName := fmt.Sprintf("nhtest%d", os.Getpid())

	// Add neighborhood server.
	nhd, err := NewLoopbackNeighborhoodServer(rootRT, serverName, addresses...)
	if err != nil {
		boom(t, "Failed to create neighborhood server: %s\n", err)
	}
	defer nhd.Stop()
	if err := server.ServeDispatcher("", nhd); err != nil {
		boom(t, "Failed to register neighborhood server: %s", err)
	}

	// Wait for the mounttable to appear in mdns
L:
	for tries := 1; tries < 2; tries++ {
		names := doGlob(t, estr, "", "*", rootRT)
		t.Logf("names %v", names)
		for _, n := range names {
			if n == serverName {
				break L
			}
		}
		time.Sleep(1 * time.Second)
	}

	// Make sure we get back a root for the server.
	want, got := []string{""}, doGlob(t, estr, serverName, "", rootRT)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected Glob result want: %q, got: %q", want, got)
	}

	// Make sure we can resolve through the neighborhood.
	expectedSuffix := "a/b"
	ctx := rootRT.NewContext()
	client := rootRT.Client()
	name := naming.JoinAddressName(estr, serverName+"/"+expectedSuffix)
	call, cerr := client.StartCall(ctx, name, "ResolveStepX", nil, options.NoResolve(true))
	if cerr != nil {
		boom(t, "ResolveStepX.StartCall: %s", cerr)
	}
	var entry naming.VDLMountEntry
	if cerr = call.Finish(&entry, &err); cerr != nil {
		boom(t, "ResolveStepX: %s", cerr)
	}

	// Resolution returned something.  Make sure its correct.
	if entry.Name != expectedSuffix {
		boom(t, "resolveStep suffix: expected %s, got %s", expectedSuffix, entry.Name)
	}
	if len(entry.Servers) == 0 {
		boom(t, "resolveStep returns no severs")
	}
L2:
	for _, s := range entry.Servers {
		for _, a := range addresses {
			if a == s.Server {
				continue L2
			}
		}
		boom(t, "Unexpected address from resolveStep result: %v", s.Server)
	}
L3:
	for _, a := range addresses {
		for _, s := range entry.Servers {
			if a == s.Server {
				continue L3
			}
		}
		boom(t, "Missing address from resolveStep result: %v", a)
	}
}
