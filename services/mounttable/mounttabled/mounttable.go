// mounttabled is a mount table daemon.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"

	"veyron/lib/signals"

	"veyron/services/mounttable/lib"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	mountName = flag.String("name", "", "Name to mount this mountable as.  Empty means don't mount.")

	aclFile = flag.String("acls", "", "ACL file. Default is to allow all access.")
	nhName  = flag.String("neighborhood_name", "", "If non-empty, publish in the local neighborhood under this name.")
)

const usage = `%s is a mount table daemon.

Usage:

  %s [--address=<local address>] [--name=<name>] [--neighborhood_name=<nh name>]

  <local address> is the the local address to listen on. By default, it will
  use a random port.

  <name>, if provided, causes the mount table to mount itself under that name.
  The name may be absolute for a remote mount table service (e.g., "/<remote mt
  address>//some/suffix") or could be relative to this process' default mount
  table (e.g., "some/suffix").

  <nh name>, if provided, will enable sharing with the local neighborhood with
  the provided name. The address of this mounttable will be published to the
  neighboorhood and everything in the neighborhood will be visible on this
  mounttable.
`

func Usage() {
	fmt.Fprintf(os.Stderr, usage, os.Args[0], os.Args[0])
}

func main() {
	flag.Usage = Usage
	r := rt.Init()
	defer r.Cleanup()

	mtServer, err := r.NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		vlog.Errorf("r.NewServer failed: %v", err)
		return
	}
	defer mtServer.Stop()
	mt, err := mounttable.NewMountTable(*aclFile)
	if err != nil {
		vlog.Errorf("r.NewMountTable failed: %v", err)
		return
	}
	mtEndpoint, err := mtServer.Listen(*protocol, *address)
	if err != nil {
		vlog.Errorf("mtServer.Listen failed: %v", err)
		return
	}
	name := *mountName
	if err := mtServer.Serve(name, mt); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", name, err)
		return
	}
	mtAddr := naming.JoinAddressName(mtEndpoint.String(), "")
	r.Namespace().SetRoots(mtAddr)

	vlog.Infof("Mount table service at: %q endpoint: %s",
		name,
		naming.JoinAddressName(mtEndpoint.String(), ""))

	if len(*nhName) > 0 {
		nhServer, err := r.NewServer(veyron2.ServesMountTableOpt(true))
		if err != nil {
			vlog.Errorf("r.NewServer failed: %v", err)
			return
		}
		defer nhServer.Stop()
		host, _, err := net.SplitHostPort(*address)
		if err != nil {
			vlog.Errorf("parsing of address(%q) failed: %v", *address, err)
			return
		}
		if _, err = nhServer.Listen(*protocol, net.JoinHostPort(host, "0")); err != nil {
			vlog.Errorf("nhServer.Listen failed: %v", err)
			return
		}
		nh, err := mounttable.NewNeighborhoodServer(*nhName, mtAddr)
		if err != nil {
			vlog.Errorf("NewNeighborhoodServer failed: %v", err)
			return
		}
		if err := nhServer.Serve("nh", nh); err != nil {
			vlog.Errorf("nhServer.Serve failed to register neighborhood: %v", err)
			return
		}
	}

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals())
}
