package main

import (
	"encoding/json"
	"fmt"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/lib/cmdline"
)

var cmdInstall = &cmdline.Command{
	Run:      runInstall,
	Name:     "install",
	Short:    "Install the given application.",
	Long:     "Install the given application.",
	ArgsName: "<device> <application> [<config override>]",
	ArgsLong: `
<device> is the veyron object name of the device manager's app service.

<application> is the veyron object name of the application.

<config override> is an optional JSON-encoded device.Config object, of the form:
   '{"flag1":"value1","flag2":"value2"}'.`,
}

func runInstall(cmd *cmdline.Command, args []string) error {
	if expectedMin, expectedMax, got := 2, 3, len(args); expectedMin > got || expectedMax < got {
		return cmd.UsageErrorf("install: incorrect number of arguments, expected between %d and %d, got %d", expectedMin, expectedMax, got)
	}
	deviceName, appName := args[0], args[1]
	var cfg device.Config
	if len(args) > 2 {
		jsonConfig := args[2]
		if err := json.Unmarshal([]byte(jsonConfig), &cfg); err != nil {
			return fmt.Errorf("Unmarshal(%v) failed: %v", jsonConfig, err)
		}
	}
	appID, err := device.ApplicationClient(deviceName).Install(gctx, appName, cfg)
	if err != nil {
		return fmt.Errorf("Install failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Successfully installed: %q\n", naming.Join(deviceName, appID))
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
	principal := veyron2.GetPrincipal(gctx)
	appInstanceIDs, err := device.ApplicationClient(appInstallation).Start(gctx, &granter{p: principal, extension: grant})
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
	Short:    "Claim the device.",
	Long:     "Claim the device.",
	ArgsName: "<device> <grant extension>",
	ArgsLong: `
<device> is the veyron object name of the device manager's app service.

<grant extension> is used to extend the default blessing of the
current principal when blessing the app instance.`,
}

func runClaim(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("claim: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName, grant := args[0], args[1]
	principal := veyron2.GetPrincipal(gctx)
	if err := device.DeviceClient(deviceName).Claim(gctx, &granter{p: principal, extension: grant}); err != nil {
		return fmt.Errorf("Claim failed: %v", err)
	}
	fmt.Fprintln(cmd.Stdout(), "Successfully claimed.")
	return nil
}

var cmdDescribe = &cmdline.Command{
	Run:      runDescribe,
	Name:     "describe",
	Short:    "Describe the device.",
	Long:     "Describe the device.",
	ArgsName: "<device>",
	ArgsLong: `
<device> is the veyron object name of the device manager's app service.`,
}

func runDescribe(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("describe: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName := args[0]
	if description, err := device.DeviceClient(deviceName).Describe(gctx); err != nil {
		return fmt.Errorf("Describe failed: %v", err)
	} else {
		fmt.Fprintf(cmd.Stdout(), "%+v\n", description)
	}
	return nil
}

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "device",
		Short: "Tool for interacting with the veyron device manager",
		Long: `
The device tool facilitates interaction with the veyron device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdStart, associateRoot(), cmdDescribe, cmdClaim, cmdStop, cmdSuspend, cmdResume, aclRoot()},
	}
}
