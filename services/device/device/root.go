// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"v.io/v23/context"

	"v.io/x/lib/cmdline"
)

var gctx *context.T

func SetGlobalContext(ctx *context.T) {
	gctx = ctx
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "device",
		Short: "facilitates interaction with the Vanadium device manager",
		Long: `
Command device facilitates interaction with the Vanadium device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdInstallLocal, cmdUninstall, associateRoot(), cmdDescribe, cmdClaim, cmdInstantiate, cmdDelete, cmdRun, cmdKill, cmdRevert, cmdUpdate, cmdUpdateAll, cmdStatus, cmdDebug, aclRoot(), cmdPublish},
	}
}
