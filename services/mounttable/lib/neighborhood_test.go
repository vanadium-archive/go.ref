package mounttable

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	_ "veyron/lib/testutil"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mounttable"
	"veyron2/vlog"
)

func protocolAndAddress(e naming.Endpoint) (string, string, error) {
	addr := e.Addr()
	if addr == nil {
		return "", "", fmt.Errorf("failed to get address")
	}
	return addr.Network(), addr.String(), nil
}

func TestNeighborhood(t *testing.T) {
	r := rt.Init()
	vlog.Infof("TestNeighborhood")
	server, err := r.NewServer()
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}

	// Start serving on a loopback address.
	e, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	estr := e.String()

	// Add neighborhood server.
	nhPrefix := "neighborhood"
	nhd, err := NewLoopbackNeighborhoodServer(nhPrefix, "joeblow", []naming.Endpoint{e})
	if err != nil {
		t.Logf("Failed to create neighborhood server: %s\n", err)
	}
	defer nhd.Stop()
	if err := server.Register(nhPrefix, nhd); err != nil {
		boom(t, "Failed to register neighborhood server: %s", err)
	}

	// Wait for the mounttable to appear in mdns
L:
	for tries := 1; tries < 2; tries++ {
		names := doGlob(t, naming.JoinAddressName(estr, "//"+nhPrefix), "*")
		t.Logf("names %v", names)
		for _, n := range names {
			if n == "joeblow" {
				break L
			}
		}
		time.Sleep(1 * time.Second)
	}

	want, got := []string{"joeblow"}, doGlob(t, naming.JoinAddressName(estr, "//neighborhood"), "*")
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected Glob result want: %q, got: %q", want, got)
	}
	want, got = []string{""}, doGlob(t, naming.JoinAddressName(estr, "//neighborhood/joeblow"), "")
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected Glob result want: %q, got: %q", want, got)
	}

	// Make sure we can resolve through the neighborhood.
	expectedSuffix := "a/b"
	objectPtr, err := mounttable.BindMountTable(naming.JoinAddressName(estr, "//neighborhood/joeblow"+"/"+expectedSuffix), quuxClient())
	if err != nil {
		boom(t, "BindMountTable: %s", err)
	}
	servers, suffix, err := objectPtr.ResolveStep()
	if err != nil {
		boom(t, "resolveStep: %s", err)
	}

	// Resolution returned something.  Make sure its correct.
	if suffix != expectedSuffix {
		boom(t, "resolveStep suffix: expected %s, got %s", expectedSuffix, suffix)
	}
	if len(servers) == 0 {
		boom(t, "resolveStep returns no severs")
	}

	for _, s := range servers {
		address, suffix := naming.SplitAddressName(s.Server)
		if len(suffix) != 0 {
			boom(t, "Endpoint %s: unexpected suffix", s.Server)
		}
		if address != e.String() {
			boom(t, "Expected %s got %s", e, address)
		}
	}
}
