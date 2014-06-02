package impl

import (
	"fmt"

	"veyron/lib/cmdline"

	"veyron2/rt"
	"veyron2/services/proximity"
)

var cmdRegister = &cmdline.Command{
	Run:      runRegister,
	Name:     "register",
	Short:    "register adds a name that the remote device will be associated with.",
	Long:     "register adds a name that the remote device will be associated with.",
	ArgsName: "<address> <name>",
	ArgsLong: `
<address> is the veyron name of the proximity server.
<name> is the name to register.
`,
}

func runRegister(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("register: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := proximity.BindProximity(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	if err = p.RegisterName(rt.R().TODOContext(), args[1]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Name registered successfully\n")
	return nil
}

var cmdUnregister = &cmdline.Command{
	Run:      runUnregister,
	Name:     "unregister",
	Short:    "unregister removes a name that the remote device it is associated with.",
	Long:     "unregister removes a name that the remote device it is associated with.",
	ArgsName: "<address> <name>",
	ArgsLong: `
<address> is the veyron name of the proximity server.
<name> is the name to unregister.
`,
}

func runUnregister(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("unregister: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := proximity.BindProximity(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	if err = p.UnregisterName(rt.R().TODOContext(), args[1]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Name unregistered successfully\n")
	return nil
}

var cmdNearbyDevices = &cmdline.Command{
	Run:      runNearbyDevices,
	Name:     "nearbydevices",
	Short:    "nearbydevices displayes the most up-to-date list of nearby devices.",
	Long:     "nearbydevices displayes the most up-to-date list of nearby devices.",
	ArgsName: "<address>",
	ArgsLong: "<address> is the veyron name of the proximity server.",
}

func runNearbyDevices(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("download: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	p, err := proximity.BindProximity(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}

	devices, err := p.NearbyDevices(rt.R().TODOContext())
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.Stdout(), "Nearby Devices:\n")
	if len(devices) == 0 {
		fmt.Fprintf(cmd.Stdout(), "None\n")
		return nil
	}

	for i, d := range devices {
		fmt.Fprintf(cmd.Stdout(), "%d: MAC=%s Names=%v Distance=%s\n", i, d.MAC, d.Names, d.Distance)
	}
	return nil
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:     "proximity",
		Short:    "Command-line tool for interacting with the Veyron proximity server",
		Long:     "Command-line tool for interacting with the Veyron proximity server",
		Children: []*cmdline.Command{cmdRegister, cmdUnregister, cmdNearbyDevices},
	}
}
