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
	"path/filepath"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/groups/internal/server"
	"v.io/x/ref/services/groups/internal/store/gkv"
	"v.io/x/ref/services/groups/internal/store/leveldb"
	"v.io/x/ref/services/groups/internal/store/mem"
)

var (
	flagName    string
	flagEngine  string
	flagRootDir string
	flagPersist string
)

func main() {
	cmdGroupsD.Flags.StringVar(&flagName, "name", "", "Name to mount the groups server as.")
	cmdGroupsD.Flags.StringVar(&flagEngine, "engine", "memstore", "Storage engine to use. Currently supported: gkv, leveldb, and memstore.")
	cmdGroupsD.Flags.StringVar(&flagRootDir, "root-dir", "/var/lib/groupsd", "Root dir for storage engines and other data.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdGroupsD)
}

// defaultPerms returns a permissions object that grants all permissions to the
// provided blessing patterns.
func defaultPerms(blessingPatterns []security.BlessingPattern) access.Permissions {
	perms := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		for _, bp := range blessingPatterns {
			perms.Add(bp, string(tag))
		}
	}
	return perms
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
	perms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		return fmt.Errorf("PermissionsFromFlag() failed: %v", err)
	}
	if perms != nil {
		ctx.Infof("Using permissions from command line flag.")
	} else {
		ctx.Infof("No permissions flag provided. Giving local principal all permissions.")
		perms = defaultPerms(security.DefaultBlessingPatterns(v23.GetPrincipal(ctx)))
	}
	var dispatcher rpc.Dispatcher
	switch flagEngine {
	case "gkv":
		file := filepath.Join(flagRootDir, ".gkvstore")
		store, err := gkv.New(file)
		if err != nil {
			ctx.Fatalf("gkv.New(%v) failed: %v", file, err)
		}
		dispatcher = server.NewManager(store, perms)
	case "leveldb":
		store, err := leveldb.Open(flagRootDir)
		if err != nil {
			ctx.Fatalf("Open(%v) failed: %v", flagRootDir, err)
		}
		dispatcher = server.NewManager(store, perms)
	case "memstore":
		dispatcher = server.NewManager(mem.New(), perms)
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
