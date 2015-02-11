// grpserverd is a group server daemon.
package main

// Example invocation:
// grpserverd --veyron.tcp.address="127.0.0.1:0" --name=grpserverd

import (
	"flag"

	"v.io/core/veyron2"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/security/groups/memstore"
	"v.io/core/veyron/services/security/groups/server"
)

// TODO(sadovsky): Perhaps this should be one of the standard Veyron flags.
var (
	name = flag.String("name", "", "Name to mount at.")
)

func main() {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	s, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Fatal("veyron2.NewServer() failed: ", err)
	}
	if _, err := s.Listen(veyron2.GetListenSpec(ctx)); err != nil {
		vlog.Fatal("s.Listen() failed: ", err)
	}

	// TODO(sadovsky): Switch to using NewAuthorizerOrDie.
	acl := access.TaggedACLMap{}
	m := server.NewManager(memstore.New(), acl)

	// Publish the service in the mount table.
	if err := s.ServeDispatcher(*name, m); err != nil {
		vlog.Fatal("s.ServeDispatcher() failed: ", err)
	}
	vlog.Info("Mounted at: ", *name)

	// Wait forever.
	<-signals.ShutdownOnSignals(ctx)
}
