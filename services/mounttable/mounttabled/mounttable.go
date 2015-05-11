// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon mounttabled implements the v.io/v23/services/mounttable interfaces.
package main

import (
	"flag"
	"fmt"
	"os"

	"v.io/v23"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/mounttable/mounttablelib"
)

var (
	mountName  = flag.String("name", "", `<name>, if provided, causes the mount table to mount itself under that name. The name may be absolute for a remote mount table service (e.g., "/<remote mt address>//some/suffix") or could be relative to this process' default mount table (e.g., "some/suffix").`)
	aclFile    = flag.String("acls", "", "AccessList file. Default is to allow all access.")
	nhName     = flag.String("neighborhood-name", "", `<nh name>, if provided, will enable sharing with the local neighborhood with the provided name. The address of this mounttable will be published to the neighboorhood and everything in the neighborhood will be visible on this mounttable.`)
	persistDir = flag.String("persist-dir", "", `Directory in which to persist permissions.`)
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	name, stop, err := mounttablelib.StartServers(ctx, v23.GetListenSpec(ctx), *mountName, *nhName, *aclFile, *persistDir, "mounttable")
	if err != nil {
		vlog.Errorf("mounttablelib.StartServers failed: %v", err)
		os.Exit(1)
	}
	defer stop()

	// Print out a directly accessible name of the mount table so that
	// integration tests can reliably read it from stdout.
	fmt.Printf("NAME=%s\n", name)

	// Wait until signal is received.
	vlog.Info("Received signal ", <-signals.ShutdownOnSignals(ctx))
}
