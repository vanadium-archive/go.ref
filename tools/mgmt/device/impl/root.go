package impl

import (
	"v.io/core/veyron2/context"

	"v.io/lib/cmdline"
)

var gctx *context.T

func SetGlobalContext(ctx *context.T) {
	gctx = ctx
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "device",
		Short: "Tool for interacting with the veyron device manager",
		Long: `
The device tool facilitates interaction with the veyron device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdInstallLocal, cmdStart, associateRoot(), cmdDescribe, cmdClaim, cmdStop, cmdSuspend, cmdResume, cmdRevert, cmdUpdate, cmdDebug, aclRoot()},
	}
}
