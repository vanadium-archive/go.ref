// mounttabled is a mount table daemon.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"

	"veyron/lib/signals"

	"veyron/services/mounttable/lib"
)

var (
	mountName = flag.String("name", "", "Name to mount this mountable as.  Empty means don't mount.")
	// TODO(rthellend): Remove the address flag when the config manager is working.
	address = flag.String("address", ":0", "Address to listen on.  Default is to use a randomly assigned port")
	prefix  = flag.String("prefix", "mt", "The prefix to register the mounttable at.")
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
  mounttable with the "nh" prefix.
`

func Usage() {
	fmt.Fprintf(os.Stderr, usage, os.Args[0], os.Args[0])
}

func main() {
	// TODO(cnicolaou): fix Usage so that it includes the flags defined by
	// the runtime
	flag.Usage = Usage
	r := rt.Init()
	defer r.Shutdown()

	server, err := r.NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		vlog.Errorf("r.NewServer failed: %v", err)
		return
	}
	defer server.Stop()
	mt, err := mounttable.NewMountTable(*aclFile)
	if err != nil {
		vlog.Errorf("r.NewMountTable failed: %v", err)
		return
	}
	if err := server.Register(*prefix, mt); err != nil {
		vlog.Errorf("server.Register failed to register mount table: %v", err)
		return
	}
	endpoint, err := server.Listen("tcp", *address)
	if err != nil {
		vlog.Errorf("server.Listen failed: %v", err)
		return
	}

	if len(*nhName) > 0 {
		mtAddr := naming.JoinAddressName(endpoint.String(), *prefix)

		nh, err := mounttable.NewNeighborhoodServer("", *nhName, mtAddr)
		if err != nil {
			vlog.Errorf("NewNeighborhoodServer failed: %v", err)
			return
		}
		nhPrefix := "nh"
		if err := server.Register(nhPrefix, nh); err != nil {
			vlog.Errorf("server.Register failed to register neighborhood: %v", err)
			return
		}
		nhAddr := naming.JoinAddressName(endpoint.String(), nhPrefix)
		nhMount := naming.Join(mtAddr, nhPrefix)

		ns := rt.R().Namespace()
		forever := time.Duration(0)
		if err = ns.Mount(rt.R().NewContext(), nhMount, nhAddr, forever); err != nil {
			vlog.Errorf("ns.Mount failed to mount neighborhood: %v", err)
			return
		}
	}

	if name := *mountName; len(name) > 0 {
		if err := server.Publish(name); err != nil {
			vlog.Errorf("Publish(%v) failed: %v", name, err)
			return
		}
		vlog.Infof("Mount table service at: %s (%s)",
			naming.Join(name, *prefix),
			naming.JoinAddressName(endpoint.String(), *prefix))

	} else {
		vlog.Infof("Mount table at: %s",
			naming.JoinAddressName(endpoint.String(), *prefix))
	}

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals())
}
