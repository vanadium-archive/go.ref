package main

import (
	"fmt"

	"veyron.io/lib/cmdline"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/node"
)

var cmdInstall = &cmdline.Command{
	Run:      runInstall,
	Name:     "install",
	Short:    "Install the given application.",
	Long:     "Install the given application.",
	ArgsName: "<node> <application>",
	ArgsLong: `
<node> is the veyron object name of the node manager's app service.
<application> is the veyron object name of the application.`,
}

func runInstall(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("install: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	nodeName, appName := args[0], args[1]
	appID, err := node.ApplicationClient(nodeName).Install(rt.R().NewContext(), appName)
	if err != nil {
		return fmt.Errorf("Install failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Successfully installed: %q\n", naming.Join(nodeName, appID))
	return nil
}

var cmdStart = &cmdline.Command{
	Run:      runStart,
	Name:     "start",
	Short:    "Start an instance of the given application.",
	Long:     "Start an instance of the given application.",
	ArgsName: "<application installation> <grant extension>",
	ArgsLong: `
<application installation> is the veyron object name of the
application installation from which to start an instance.

<grant extension> is used to extend the default blessing of the
current principal when blessing the app instance.`,
}

type granter struct {
	ipc.CallOpt
	p         security.Principal
	extension string
}

func (g *granter) Grant(other security.Blessings) (security.Blessings, error) {
	return g.p.Bless(other.PublicKey(), g.p.BlessingStore().Default(), g.extension, security.UnconstrainedUse())
}

func runStart(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("start: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appInstallation, grant := args[0], args[1]
	appInstanceIDs, err := node.ApplicationClient(appInstallation).Start(rt.R().NewContext(), &granter{p: rt.R().Principal(), extension: grant})
	if err != nil {
		return fmt.Errorf("Start failed: %v", err)
	}
	for _, id := range appInstanceIDs {
		fmt.Fprintf(cmd.Stdout(), "Successfully started: %q\n", naming.Join(appInstallation, id))
	}
	return nil
}

var cmdClaim = &cmdline.Command{
	Run:      runClaim,
	Name:     "claim",
	Short:    "Claim the node.",
	Long:     "Claim the node.",
	ArgsName: "<node> <grant extension>",
	ArgsLong: `
<node> is the veyron object name of the node manager's app service.

<grant extension> is used to extend the default blessing of the
current principal when blessing the app instance.`,
}

func runClaim(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("claim: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	nodeName, grant := args[0], args[1]
	if err := node.NodeClient(nodeName).Claim(rt.R().NewContext(), &granter{p: rt.R().Principal(), extension: grant}); err != nil {
		return fmt.Errorf("Claim failed: %v", err)
	}
	fmt.Fprintln(cmd.Stdout(), "Successfully claimed.")
	return nil
}

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "nodex",
		Short: "Tool for interacting with the veyron node manager",
		Long: `
The nodex tool facilitates interaction with the veyron node manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdStart, associateRoot(), cmdClaim, cmdStop, cmdSuspend, cmdResume, aclRoot()},
	}
}
