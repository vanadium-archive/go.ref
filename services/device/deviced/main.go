// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon deviced implements the v.io/v23/services/device interfaces.
package main

import (
	"os"
	"runtime"

	"v.io/x/lib/cmdline"
)

func main() {
	// TODO(caprita): Remove this once we have a way to set the GOMAXPROCS
	// environment variable persistently for device manager.
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	rootCmd := cmdline.Command{
		Name:  "deviced",
		Short: "Vanadium device manager setup",
		Long: `
deviced can be used to launch, configure, or manage the device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdUninstall, cmdStart, cmdStop, cmdProfile},
		Run:      runServer,
	}
	os.Exit(rootCmd.Main())
}
