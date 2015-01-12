package main

import (
	"fmt"
	"os"

	"v.io/lib/cmdline"

	"v.io/core/veyron/services/mgmt/device/impl"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"
)

var (
	installFrom string
	suidHelper  string
	agent       string
	singleUser  bool
	sessionMode bool
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
	cmdInstall.Flags.BoolVar(&singleUser, "single_user", false, "if set, performs the installation assuming a single-user system")
	cmdInstall.Flags.BoolVar(&sessionMode, "session_mode", false, "if set, installs the device manager to run a single session. Otherwise, the device manager is configured to get restarted upon exit")
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
	if err := impl.SelfInstall(installationDir(), suidHelper, agent, singleUser, sessionMode, args, os.Environ()); err != nil {
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

func runUninstall(*cmdline.Command, []string) error {
	if err := impl.Uninstall(installationDir()); err != nil {
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

func runStart(*cmdline.Command, []string) error {
	if err := impl.Start(installationDir(), nil, nil); err != nil {
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

func runStop(*cmdline.Command, []string) error {
	runtime, err := rt.New()
	if err != nil {
		vlog.Errorf("Could not initialize runtime: %v", err)
		return err
	}
	defer runtime.Cleanup()
	if err := impl.Stop(runtime.NewContext(), installationDir()); err != nil {
		vlog.Errorf("Stop failed: %v", err)
		return err
	}
	return nil
}
