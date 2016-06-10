// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"math/rand"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/mounttable/btmtd/internal"
)

var (
	cmdRoot = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runMT),
		Name:   "btmtd",
		Short:  "Runs the mounttable service",
		Long:   "Runs the mounttable service.",
		Children: []*cmdline.Command{
			cmdSetup, cmdDestroy, cmdDump,
		},
	}
	cmdSetup = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runSetup),
		Name:   "setup",
		Short:  "Creates and sets up the table",
		Long:   "Creates and sets up the table.",
	}
	cmdDestroy = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runDestroy),
		Name:   "destroy",
		Short:  "Destroy the table",
		Long:   "Destroy the table. All data will be lost.",
	}
	cmdDump = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runDump),
		Name:   "dump",
		Short:  "Dump the table",
		Long:   "Dump the table.",
	}

	keyFileFlag      string
	projectFlag      string
	zoneFlag         string
	clusterFlag      string
	tableFlag        string
	inMemoryTestFlag bool

	permissionsFileFlag string
)

func main() {
	rand.Seed(time.Now().UnixNano())
	cmdRoot.Flags.StringVar(&keyFileFlag, "key-file", "", "The file that contains the Google Cloud JSON credentials to use")
	cmdRoot.Flags.StringVar(&projectFlag, "project", "", "The Google Cloud project of the Cloud Bigtable cluster")
	cmdRoot.Flags.StringVar(&zoneFlag, "zone", "", "The Google Cloud zone of the Cloud Bigtable cluster")
	cmdRoot.Flags.StringVar(&clusterFlag, "cluster", "", "The Cloud Bigtable cluster name")
	cmdRoot.Flags.StringVar(&tableFlag, "table", "mounttable", "The name of the table to use")
	cmdRoot.Flags.BoolVar(&inMemoryTestFlag, "in-memory-test", false, "If true, use an in-memory bigtable server (for testing only)")
	cmdRoot.Flags.StringVar(&permissionsFileFlag, "permissions-file", "", "The file that contains the initial node permissions.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

func runSetup(ctx *context.T, env *cmdline.Env, args []string) error {
	bt, err := internal.NewBigTable(keyFileFlag, projectFlag, zoneFlag, clusterFlag, tableFlag)
	if err != nil {
		return err
	}
	return bt.SetupTable(ctx, permissionsFileFlag)
}

func runDestroy(ctx *context.T, env *cmdline.Env, args []string) error {
	bt, err := internal.NewBigTable(keyFileFlag, projectFlag, zoneFlag, clusterFlag, tableFlag)
	if err != nil {
		return err
	}
	return bt.DeleteTable(ctx)
}

func runDump(ctx *context.T, env *cmdline.Env, args []string) error {
	bt, err := internal.NewBigTable(keyFileFlag, projectFlag, zoneFlag, clusterFlag, tableFlag)
	if err != nil {
		return err
	}
	return bt.DumpTable(ctx)
}

func runMT(ctx *context.T, env *cmdline.Env, args []string) error {
	var (
		bt  *internal.BigTable
		err error
	)
	if inMemoryTestFlag {
		var shutdown func()
		if bt, shutdown, err = internal.NewTestBigTable(tableFlag); err != nil {
			return err
		}
		defer shutdown()
		if err := bt.SetupTable(ctx, permissionsFileFlag); err != nil {
			return err
		}
	} else {
		if bt, err = internal.NewBigTable(keyFileFlag, projectFlag, zoneFlag, clusterFlag, tableFlag); err != nil {
			return err
		}
	}

	globalPerms, err := securityflag.PermissionsFromFlag()
	if err != nil {
		return err
	}
	acl := globalPerms["Admin"]
	disp := internal.NewDispatcher(bt, &acl)
	ctx, server, err := v23.WithNewDispatchingServer(ctx, "", disp, options.ServesMountTable(true))
	if err != nil {
		return err
	}
	ctx.Infof("Listening on: %v", server.Status().Endpoints)

	<-signals.ShutdownOnSignals(ctx)
	return nil
}
