package main

import (
	"fmt"
	"os"

	"v.io/lib/cmdline"

	"v.io/core/veyron/services/mgmt/device/impl"
	"v.io/v23"
	"v.io/v23/vlog"
)

var (
	installFrom string
	suidHelper  string
	agent       string
	initHelper  string
	origin      string
	singleUser  bool
	sessionMode bool
	initMode    bool
)

const deviceDirEnv = "VANADIUM_DEVICE_DIR"

func installationDir() string {
	if d := os.Getenv(deviceDirEnv); d != "" {
		return d
	}
	if d, err := os.Getwd(); err != nil {
		vlog.Errorf("Failed to get current dir: %v", err)
		return ""
	} else {
		return d
	}
}

var cmdInstall = &cmdline.Command{
	Run:      runInstall,
	Name:     "install",
	Short:    "Install the device manager.",
	Long:     fmt.Sprintf("Performs installation of device manager into %s (if the env var set), or into the current dir otherwise", deviceDirEnv),
	ArgsName: "[-- <arguments for device manager>]",
	ArgsLong: `
Arguments to be passed to the installed device manager`,
}

func init() {
	cmdInstall.Flags.StringVar(&installFrom, "from", "", "if specified, performs the installation from the provided application envelope object name")
	cmdInstall.Flags.StringVar(&suidHelper, "suid_helper", "", "path to suid helper")
	cmdInstall.Flags.StringVar(&agent, "agent", "", "path to security agent")
	cmdInstall.Flags.StringVar(&initHelper, "init_helper", "", "path to sysinit helper")
	cmdInstall.Flags.StringVar(&origin, "origin", "", "if specified, self-updates will use this origin")
	cmdInstall.Flags.BoolVar(&singleUser, "single_user", false, "if set, performs the installation assuming a single-user system")
	cmdInstall.Flags.BoolVar(&sessionMode, "session_mode", false, "if set, installs the device manager to run a single session. Otherwise, the device manager is configured to get restarted upon exit")
	cmdInstall.Flags.BoolVar(&initMode, "init_mode", false, "if set, installs the device manager with the system init service manager")
}

func runInstall(cmd *cmdline.Command, args []string) error {
	if installFrom != "" {
		// TODO(caprita): Also pass args into InstallFrom.
		if err := impl.InstallFrom(installFrom); err != nil {
			vlog.Errorf("InstallFrom(%v) failed: %v", installFrom, err)
			return err
		}
		return nil
	}
	if suidHelper == "" {
		return cmd.UsageErrorf("--suid_helper must be set")
	}
	if agent == "" {
		return cmd.UsageErrorf("--agent must be set")
	}
	if initMode && initHelper == "" {
		return cmd.UsageErrorf("--init_helper must be set")
	}
	if err := impl.SelfInstall(installationDir(), suidHelper, agent, initHelper, origin, singleUser, sessionMode, initMode, args, os.Environ(), cmd.Stderr(), cmd.Stdout()); err != nil {
		vlog.Errorf("SelfInstall failed: %v", err)
		return err
	}
	return nil
}

var cmdUninstall = &cmdline.Command{
	Run:   runUninstall,
	Name:  "uninstall",
	Short: "Uninstall the device manager.",
	Long:  fmt.Sprintf("Removes the device manager installation from %s (if the env var set), or the current dir otherwise", deviceDirEnv),
}

func init() {
	cmdUninstall.Flags.StringVar(&suidHelper, "suid_helper", "", "path to suid helper")
}

func runUninstall(cmd *cmdline.Command, _ []string) error {
	if suidHelper == "" {
		return cmd.UsageErrorf("--suid_helper must be set")
	}
	if err := impl.Uninstall(installationDir(), suidHelper, cmd.Stderr(), cmd.Stdout()); err != nil {
		vlog.Errorf("Uninstall failed: %v", err)
		return err
	}
	return nil
}

var cmdStart = &cmdline.Command{
	Run:   runStart,
	Name:  "start",
	Short: "Start the device manager.",
	Long:  fmt.Sprintf("Starts the device manager installed under from %s (if the env var set), or the current dir otherwise", deviceDirEnv),
}

func runStart(cmd *cmdline.Command, _ []string) error {
	if err := impl.Start(installationDir(), cmd.Stderr(), cmd.Stdout()); err != nil {
		vlog.Errorf("Start failed: %v", err)
		return err
	}
	return nil
}

var cmdStop = &cmdline.Command{
	Run:   runStop,
	Name:  "stop",
	Short: "Stop the device manager.",
	Long:  fmt.Sprintf("Stops the device manager installed under from %s (if the env var set), or the current dir otherwise", deviceDirEnv),
}

func runStop(cmd *cmdline.Command, _ []string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()
	if err := impl.Stop(ctx, installationDir(), cmd.Stderr(), cmd.Stdout()); err != nil {
		vlog.Errorf("Stop failed: %v", err)
		return err
	}
	return nil
}

var cmdProfile = &cmdline.Command{
	Run:   runProfile,
	Name:  "profile",
	Short: "Dumps profile for the device manager.",
	Long:  "Prints the internal profile description for the device manager.",
}

func runProfile(cmd *cmdline.Command, _ []string) error {
	spec, err := impl.ComputeDeviceProfile()
	if err != nil {
		vlog.Errorf("ComputeDeviceProfile failed: %v", err)
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Profile: %#v\n", spec)
	desc, err := impl.Describe()
	if err != nil {
		vlog.Errorf("Describe failed: %v", err)
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Description: %#v\n", desc)
	return nil
}
