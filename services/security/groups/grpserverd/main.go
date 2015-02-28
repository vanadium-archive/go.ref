// grpserverd is a group server daemon.
package main

// Example invocation:
// grpserverd --veyron.tcp.address="127.0.0.1:0" --name=grpserverd

import (
	"flag"

	"v.io/v23"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles/roaming"
	"v.io/x/ref/services/security/groups/memstore"
	"v.io/x/ref/services/security/groups/server"
)

// TODO(sadovsky): Perhaps this should be one of the standard Veyron flags.
var (
	name = flag.String("name", "", "Name to mount at.")
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("v23.NewServer() failed: ", err)
	}
	if _, err := s.Listen(v23.GetListenSpec(ctx)); err != nil {
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
