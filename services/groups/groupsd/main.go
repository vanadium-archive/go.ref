// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon groupsd implements the v.io/v23/services/groups interfaces for
// managing access control groups.
package main

// Example invocation:
// groupsd --v23.tcp.address="127.0.0.1:0" --name=groupsd

import (
	"fmt"
	"strings"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/conventions"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/groups/internal/server"
	"v.io/x/ref/services/groups/internal/store/leveldb"
	"v.io/x/ref/services/groups/internal/store/mem"
)

var (
	flagName    string
	flagEngine  string
	flagRootDir string
	flagPersist string

	errNotAuthorizedToCreate = verror.Register("v.io/x/ref/services/groups/groupsd.errNotAuthorizedToCreate", verror.NoRetry, "{1} {2} Creator user ids {3} are not authorized to create group {4}, group name must begin with one of the user ids")
)

func main() {
	cmdGroupsD.Flags.StringVar(&flagName, "name", "", "Name to mount the groups server as.")
	cmdGroupsD.Flags.StringVar(&flagEngine, "engine", "memstore", "Storage engine to use. Currently supported: leveldb, and memstore.")
	cmdGroupsD.Flags.StringVar(&flagRootDir, "root-dir", "/var/lib/groupsd", "Root dir for storage engines and other data.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdGroupsD)
}

// Authorizer implementing the authorization policy for Create operations.
//
// A user is allowed to create any group that begins with the user id.
//
// TODO(ashankar): This is experimental use of the "conventions" API and of a
// creation policy. This policy was thought of in a 5 minute period. Think
// about this more!
type createAuthorizer struct{}

func (createAuthorizer) Authorize(ctx *context.T, call security.Call) error {
	userids := conventions.GetClientUserIds(ctx, call)
	for _, uid := range userids {
		if strings.HasPrefix(call.Suffix(), uid+"/") {
			return nil
		}
	}
	// Revert to the default authorization policy.
	if err := security.DefaultAuthorizer().Authorize(ctx, call); err == nil {
		return nil
	}
	return verror.New(errNotAuthorizedToCreate, ctx, userids, call.Suffix())
}

var cmdGroupsD = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runGroupsD),
	Name:   "groupsd",
	Short:  "Runs the groups daemon.",
	Long: `
Command groupsd runs the groups daemon, which implements the
v.io/v23/services/groups.Group interface.
`,
}

func runGroupsD(ctx *context.T, env *cmdline.Env, args []string) error {
	var dispatcher rpc.Dispatcher
	switch flagEngine {
	case "leveldb":
		store, err := leveldb.Open(flagRootDir)
		if err != nil {
			ctx.Fatalf("Open(%v) failed: %v", flagRootDir, err)
		}
		dispatcher = server.NewManager(store, createAuthorizer{})
	case "memstore":
		dispatcher = server.NewManager(mem.New(), createAuthorizer{})
	default:
		return fmt.Errorf("unknown storage engine %v", flagEngine)
	}
	ctx, server, err := v23.WithNewDispatchingServer(ctx, flagName, dispatcher)
	if err != nil {
		return fmt.Errorf("NewDispatchingServer(%v) failed: %v", flagName, err)
	}
	ctx.Infof("Groups server running at endpoint=%q", server.Status().Endpoints[0].Name())

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
