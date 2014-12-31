package main

import (
	"os"

	"v.io/lib/cmdline"

	"v.io/core/veyron/services/mgmt/device/impl"
	"v.io/core/veyron2/vlog"
)

var installFrom string

var cmdInstall = &cmdline.Command{
	Run:      runInstall,
	Name:     "install",
	Short:    "Install the device manager.",
	Long:     "Performs installation of device manager based on the config specified via the environment.",
	ArgsName: "[-- <arguments for device manager>]",
	ArgsLong: `
Arguments to be passed to the installed device manager`,
}

func init() {
	cmdInstall.Flags.StringVar(&installFrom, "from", "", "if specified, performs the installation from the provided application envelope object name")
}

func runInstall(_ *cmdline.Command, args []string) error {
	if installFrom != "" {
		// TODO(caprita): Also pass args into InstallFrom.
		if err := impl.InstallFrom(installFrom); err != nil {
			vlog.Errorf("InstallFrom(%v) failed: %v", installFrom, err)
			return err
		}
		return nil
	}
	if err := impl.SelfInstall(args, os.Environ()); err != nil {
		vlog.Errorf("SelfInstall failed: %v", err)
		return err
	}
	return nil
}

var cmdUninstall = &cmdline.Command{
	Run:   runUninstall,
	Name:  "uninstall",
	Short: "Uninstall the device manager.",
	Long:  "Removes the device manager installation based on the config specified via the environment.",
}

func runUninstall(*cmdline.Command, []string) error {
	if err := impl.Uninstall(); err != nil {
		vlog.Errorf("Uninstall failed: %v", err)
		return err
	}
	return nil
}
