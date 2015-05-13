// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/mounttable/mounttablelib"
)

var mountName, aclFile, nhName, persistDir string

func main() {
	cmdMTD.Flags.StringVar(&mountName, "name", "", `If provided, causes the mount table to mount itself under this name.  The name may be absolute for a remote mount table service (e.g. "/<remote mt address>//some/suffix") or could be relative to this process' default mount table (e.g. "some/suffix").`)
	cmdMTD.Flags.StringVar(&aclFile, "acls", "", "AccessList file.  Default is to allow all access.")
	cmdMTD.Flags.StringVar(&nhName, "neighborhood-name", "", `If provided, enables sharing with the local neighborhood with the provided name.  The address of this mounttable will be published to the neighboorhood and everything in the neighborhood will be visible on this mounttable.`)
	cmdMTD.Flags.StringVar(&persistDir, "persist-dir", "", `Directory in which to persist permissions.`)

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdMTD)
}

var cmdMTD = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runMountTableD),
	Name:   "mounttabled",
	Short:  "Runs the mounttable interface daemon",
	Long: `
Command mounttabled runs the mounttable daemon, which implements the
v.io/v23/services/mounttable interfaces.
`,
}

func runMountTableD(ctx *context.T, env *cmdline.Env, args []string) error {
	name, stop, err := mounttablelib.StartServers(ctx, v23.GetListenSpec(ctx), mountName, nhName, aclFile, persistDir, "mounttable")
	if err != nil {
		return fmt.Errorf("mounttablelib.StartServers failed: %v", err)
	}
	defer stop()

	// Print out a directly accessible name of the mount table so that
	// integration tests can reliably read it from stdout.
	fmt.Printf("NAME=%s\n", name)

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals(ctx))
	return nil
}
