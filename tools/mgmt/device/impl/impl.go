package impl

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
	ArgsName: "<device> <application>",
	ArgsLong: `
<device> is the veyron object name of the device manager's app service.

<application> is the veyron object name of the application.
`,
}

type configFlag device.Config

func (c *configFlag) String() string {
	jsonConfig, _ := json.Marshal(c)
	return string(jsonConfig)
}
func (c *configFlag) Set(s string) error {
	if err := json.Unmarshal([]byte(s), c); err != nil {
		return fmt.Errorf("Unmarshal(%v) failed: %v", s, err)
	}
	return nil
}

var configOverride configFlag = configFlag{}

func init() {
	cmdInstall.Flags.Var(&configOverride, "config", "JSON-encoded device.Config object, of the form: '{\"flag1\":\"value1\",\"flag2\":\"value2\"}'")
}

func runInstall(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("install: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName, appName := args[0], args[1]
	appID, err := device.ApplicationClient(deviceName).Install(gctx, appName, device.Config(configOverride))
	// Reset the value for any future invocations of "install" or
	// "install-local" (we run more than one command per process in unit
	// tests).
	configOverride = configFlag{}
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
<device> is the veyron object name of the device manager's device service.

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
<device> is the veyron object name of the device manager's device service.`,
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

var cmdUpdate = &cmdline.Command{
	Run:      runUpdate,
	Name:     "update",
	Short:    "Update the device manager or application",
	Long:     "Update the device manager or application",
	ArgsName: "<object>",
	ArgsLong: `
<object> is the veyron object name of the device manager or application
installation to update.`,
}

func runUpdate(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("update: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName := args[0]
	if err := device.ApplicationClient(deviceName).Update(gctx); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Update successful.")
	return nil
}

var cmdRevert = &cmdline.Command{
	Run:      runRevert,
	Name:     "revert",
	Short:    "Revert the device manager or application",
	Long:     "Revert the device manager or application to its previous version",
	ArgsName: "<object>",
	ArgsLong: `
<object> is the veyron object name of the device manager or application
installation to revert.`,
}

func runRevert(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("revert: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName := args[0]
	if err := device.ApplicationClient(deviceName).Revert(gctx); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Revert successful.")
	return nil
}

var cmdDebug = &cmdline.Command{
	Run:      runDebug,
	Name:     "debug",
	Short:    "Debug the device.",
	Long:     "Debug the device.",
	ArgsName: "<device>",
	ArgsLong: `
<device> is the veyron object name of an app installation or instance.`,
}

func runDebug(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("debug: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName := args[0]
	if description, err := device.DeviceClient(deviceName).Debug(gctx); err != nil {
		return fmt.Errorf("Debug failed: %v", err)
	} else {
		fmt.Fprintf(cmd.Stdout(), "%v\n", description)
	}
	return nil
}
