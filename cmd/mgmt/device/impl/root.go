// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

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
		Short: "Tool for interacting with the vanadium device manager",
		Long: `
The device tool facilitates interaction with the vanadium device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdInstallLocal, cmdUninstall, cmdStart, associateRoot(), cmdDescribe, cmdClaim, cmdStop, cmdSuspend, cmdResume, cmdRevert, cmdUpdate, cmdUpdateAll, cmdDebug, aclRoot(), cmdPublish},
	}
}
