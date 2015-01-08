// mounttabled is a mount table daemon.
package main

import (
	"flag"
	"net"
	"os"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	mounttable "v.io/core/veyron/services/mounttable/lib"
)

var (
	mountName = flag.String("name", "", `<name>, if provided, causes the mount table to mount itself under that name. The name may be absolute for a remote mount table service (e.g., "/<remote mt address>//some/suffix") or could be relative to this process' default mount table (e.g., "some/suffix").`)
	aclFile   = flag.String("acls", "", "ACL file. Default is to allow all access.")
	nhName    = flag.String("neighborhood_name", "", `<nh name>, if provided, will enable sharing with the local neighborhood with the provided name. The address of this mounttable will be published to the neighboorhood and everything in the neighborhood will be visible on this mounttable.`)
)

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer r.Cleanup()

	mtServer, err := r.NewServer(options.ServesMountTable(true))
	if err != nil {
		vlog.Errorf("r.NewServer failed: %v", err)
		os.Exit(1)
	}
	defer mtServer.Stop()
	mt, err := mounttable.NewMountTable(*aclFile)
	if err != nil {
		vlog.Errorf("r.NewMountTable failed: %v", err)
		os.Exit(1)
	}
	mtEndpoints, err := mtServer.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("mtServer.Listen failed: %v", err)
		os.Exit(1)
	}
	mtEndpoint := mtEndpoints[0]
	name := *mountName
	if err := mtServer.ServeDispatcher(name, mt); err != nil {
		vlog.Errorf("ServeDispatcher(%v) failed: %v", name, err)
		os.Exit(1)
	}

	vlog.Infof("Mount table service at: %q endpoint: %s",
		name,
		naming.JoinAddressName(mtEndpoint.String(), ""))

	if len(*nhName) > 0 {
		neighborhoodListenSpec := roaming.ListenSpec
		// The ListenSpec code ensures that we have a valid address here.
		host, port, _ := net.SplitHostPort(roaming.ListenSpec.Addrs[0].Address)
		if port != "" {
			neighborhoodListenSpec.Addrs[0].Address = net.JoinHostPort(host, "0")
		}
		nhServer, err := r.NewServer(options.ServesMountTable(true))
		if err != nil {
			vlog.Errorf("r.NewServer failed: %v", err)
			os.Exit(1)
		}
		defer nhServer.Stop()
		if _, err := nhServer.Listen(neighborhoodListenSpec); err != nil {
			vlog.Errorf("nhServer.Listen failed: %v", err)
			os.Exit(1)
		}

		myObjectName := naming.JoinAddressName(mtEndpoint.String(), "")

		nh, err := mounttable.NewNeighborhoodServer(*nhName, myObjectName)
		if err != nil {
			vlog.Errorf("NewNeighborhoodServer failed: %v", err)
			os.Exit(1)
		}
		if err := nhServer.ServeDispatcher(naming.JoinAddressName(myObjectName, "nh"), nh); err != nil {
			vlog.Errorf("nhServer.ServeDispatcher failed to register neighborhood: %v", err)
			os.Exit(1)
		}
	}

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals(r))
}
