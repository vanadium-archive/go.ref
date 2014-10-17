// mounttabled is a mount table daemon.
package main

import (
	"flag"
	"net"
	"os"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	mounttable "veyron.io/veyron/veyron/services/mounttable/lib"
)

var (
	mountName = flag.String("name", "", `<name>, if provided, causes the mount table to mount itself under that name. The name may be absolute for a remote mount table service (e.g., "/<remote mt address>//some/suffix") or could be relative to this process' default mount table (e.g., "some/suffix").`)
	aclFile   = flag.String("acls", "", "ACL file. Default is to allow all access.")
	nhName    = flag.String("neighborhood_name", "", `<nh name>, if provided, will enable sharing with the local neighborhood with the provided name. The address of this mounttable will be published to the neighboorhood and everything in the neighborhood will be visible on this mounttable.`)
)

func main() {
	r := rt.Init()
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
	mtEndpoint, err := mtServer.ListenX(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("mtServer.Listen failed: %v", err)
		os.Exit(1)
	}
	name := *mountName
	if err := mtServer.Serve(name, mt); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", name, err)
		os.Exit(1)
	}

	vlog.Infof("Mount table service at: %q endpoint: %s",
		name,
		naming.JoinAddressName(mtEndpoint.String(), ""))

	if len(*nhName) > 0 {
		neighborhoodListenSpec := *roaming.ListenSpec
		// The ListenSpec code ensures that we have a valid address here.
		host, port, _ := net.SplitHostPort(roaming.ListenSpec.Address)
		if port != "" {
			neighborhoodListenSpec.Address = net.JoinHostPort(host, "0")
		}
		nhServer, err := r.NewServer(options.ServesMountTable(true))
		if err != nil {
			vlog.Errorf("r.NewServer failed: %v", err)
			os.Exit(1)
		}
		defer nhServer.Stop()
		if _, err := nhServer.ListenX(&neighborhoodListenSpec); err != nil {
			vlog.Errorf("nhServer.Listen failed: %v", err)
			os.Exit(1)
		}

		myObjectName := naming.JoinAddressName(mtEndpoint.String(), "")

		nh, err := mounttable.NewNeighborhoodServer(*nhName, myObjectName)
		if err != nil {
			vlog.Errorf("NewNeighborhoodServer failed: %v", err)
			os.Exit(1)
		}
		if err := nhServer.Serve(naming.JoinAddressName(myObjectName, "//nh"), nh); err != nil {
			vlog.Errorf("nhServer.Serve failed to register neighborhood: %v", err)
			os.Exit(1)
		}
	}

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals())
}
