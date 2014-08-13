package main

import (
	"flag"

	"veyron/lib/signals"
	vsecurity "veyron/security"
	"veyron/services/security/discharger"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	aclFile = flag.String("discharger-acl", "", "ACL to use for the discharge service")
	publish = flag.String("publish", "discharger", "the Object Name under which to publish this service")

	storeName      = flag.String("revocation-store", "", "Object Name of the Veyron store to be used for revocation. Omit to disable revocation functionality.")
	publishRevoker = flag.String("publish-revoker", "revoker", "the Object Name under which to publish this service")
	pathInStore    = flag.String("path-in-store", "/revoker", "the location in store where the revoker keeps its state")
	revokerAclFile = flag.String("revoker-acl", "", "ACL to use for the revocation service")
)

func authorizer(file string) security.Authorizer {
	if file == "" {
		return vsecurity.NewACLAuthorizer(security.ACL{security.AllPrincipals: security.AllLabels})
	}
	return vsecurity.NewFileACLAuthorizer(file)
}

func main() {
	r := rt.Init()
	defer r.Cleanup()

	dischargerServer, err := r.NewServer()
	if err != nil {
		vlog.Fatal(err)
	}
	defer dischargerServer.Stop()
	dischargerEndpoint, err := dischargerServer.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatal(err)
	}
	if err = dischargerServer.Serve(*publish, ipc.SoloDispatcher(discharger.New(r.Identity()), authorizer(*aclFile))); err != nil {
		vlog.Fatal(err)
	}
	vlog.Infof("discharger: %s", dischargerEndpoint.String())

	if *storeName != "" {
		revokerServer, err := r.NewServer()
		if err != nil {
			vlog.Fatal(err)
		}
		defer revokerServer.Stop()
		revokerEndpoint, err := revokerServer.Listen(*protocol, *address)
		if err != nil {
			vlog.Fatal(err)
		}
		revokerService, err := discharger.NewRevoker(*storeName, *pathInStore)
		if err != nil {
			vlog.Fatal(err)
		}
		err = revokerServer.Serve(*publish, ipc.SoloDispatcher(revokerService, authorizer(*revokerAclFile)))
		if err != nil {
			vlog.Fatal(err)
		}
		vlog.Infof("revoker: %s", revokerEndpoint.String())
	}

	<-signals.ShutdownOnSignals()
}
