// mounttabled is a simple mount table daemon.
package main

import (
	"flag"
	"fmt"
	"os"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"

	"veyron/lib/signals"

	"veyron/services/mounttable/lib"
)

var (
	mountName = flag.String("name", "", "Name to mount this mountable as.  Empty means don't mount.")
	// TODO(rthellend): Remove the address flag when the config manager is working.
	address   = flag.String("address", ":0", "Address to listen on.  Default is to use a randomly assigned port")
	prefix    = flag.String("prefix", "mt", "The prefix to register the server at.")
	aclFile = flag.String("acls", "", "ACL file. Default is to allow all access.")
)

const usage = `%s is a simple mount table daemon.

Usage:

  %s [--name=<name>]

  <name>, if provided, causes the mount table to mount itself under that name.
  The name may be absolute for a remote mount table service (e.g., "/<remote mt
  address>//some/suffix") or could be relative to this process' default mount
  table (e.g., "some/suffix").
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

	server, err := r.NewServer()
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

	if name := *mountName; len(name) > 0 {
		if err := server.Publish(name); err != nil {
			vlog.Errorf("Publish(%v) failed: %v", name, err)
			return
		}
		vlog.Infof("Mount table service at: %s (%s)",
			naming.JoinAddressName(name, *prefix),
			naming.JoinAddressName(endpoint.String(), *prefix))

	} else {
		vlog.Infof("Mount table at: %s",
			naming.JoinAddressName(endpoint.String(), *prefix))
	}

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals())
}
