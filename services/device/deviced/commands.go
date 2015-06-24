// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"v.io/x/lib/cmdline"

	"v.io/v23"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/device/internal/impl"
	"v.io/x/ref/services/device/internal/installer"
)

var (
	installFrom string
	suidHelper  string
	agent       string
	initHelper  string
	devUserName string
	origin      string
	singleUser  bool
	sessionMode bool
	initMode    bool
)

const deviceDirEnv = "V23_DEVICE_DIR"

func installationDir(env *cmdline.Env) string {
	if d := env.Vars[deviceDirEnv]; d != "" {
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
	Runner:   cmdline.RunnerFunc(runInstall),
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
	cmdInstall.Flags.StringVar(&devUserName, "devuser", "", "if specified, device manager will run as this user. Provided by devicex but ignored .")
	cmdInstall.Flags.BoolVar(&singleUser, "single_user", false, "if set, performs the installation assuming a single-user system")
	cmdInstall.Flags.BoolVar(&sessionMode, "session_mode", false, "if set, installs the device manager to run a single session. Otherwise, the device manager is configured to get restarted upon exit")
	cmdInstall.Flags.BoolVar(&initMode, "init_mode", false, "if set, installs the device manager with the system init service manager")
}

func runInstall(env *cmdline.Env, args []string) error {
	if installFrom != "" {
		// TODO(caprita): Also pass args into InstallFrom.
		if err := installer.InstallFrom(installFrom); err != nil {
			vlog.Errorf("InstallFrom(%v) failed: %v", installFrom, err)
			return err
		}
		return nil
	}
	if suidHelper == "" {
		return env.UsageErrorf("--suid_helper must be set")
	}
	if agent == "" {
		return env.UsageErrorf("--agent must be set")
	}
	if initMode && initHelper == "" {
		return env.UsageErrorf("--init_helper must be set")
	}
	if err := installer.SelfInstall(installationDir(env), suidHelper, agent, initHelper, origin, singleUser, sessionMode, initMode, args, os.Environ(), env.Stderr, env.Stdout); err != nil {
		vlog.Errorf("SelfInstall failed: %v", err)
		return err
	}
	return nil
}

var cmdUninstall = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runUninstall),
	Name:   "uninstall",
	Short:  "Uninstall the device manager.",
	Long:   fmt.Sprintf("Removes the device manager installation from %s (if the env var set), or the current dir otherwise", deviceDirEnv),
}

func init() {
	cmdUninstall.Flags.StringVar(&suidHelper, "suid_helper", "", "path to suid helper")
}

func runUninstall(env *cmdline.Env, _ []string) error {
	if suidHelper == "" {
		return env.UsageErrorf("--suid_helper must be set")
	}
	if err := installer.Uninstall(installationDir(env), suidHelper, env.Stderr, env.Stdout); err != nil {
		vlog.Errorf("Uninstall failed: %v", err)
		return err
	}
	return nil
}

var cmdStart = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runStart),
	Name:   "start",
	Short:  "Start the device manager.",
	Long:   fmt.Sprintf("Starts the device manager installed under from %s (if the env var set), or the current dir otherwise", deviceDirEnv),
}

func runStart(env *cmdline.Env, _ []string) error {
	if err := installer.Start(installationDir(env), env.Stderr, env.Stdout); err != nil {
		vlog.Errorf("Start failed: %v", err)
		return err
	}
	return nil
}

var cmdStop = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runStop),
	Name:   "stop",
	Short:  "Stop the device manager.",
	Long:   fmt.Sprintf("Stops the device manager installed under from %s (if the env var set), or the current dir otherwise", deviceDirEnv),
}

func runStop(env *cmdline.Env, _ []string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()
	if err := installer.Stop(ctx, installationDir(env), env.Stderr, env.Stdout); err != nil {
		vlog.Errorf("Stop failed: %v", err)
		return err
	}
	return nil
}

var cmdProfile = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runProfile),
	Name:   "profile",
	Short:  "Dumps profile for the device manager.",
	Long:   "Prints the internal profile description for the device manager.",
}

func runProfile(env *cmdline.Env, _ []string) error {
	spec, err := impl.ComputeDeviceProfile()
	if err != nil {
		vlog.Errorf("ComputeDeviceProfile failed: %v", err)
		return err
	}
	fmt.Fprintf(env.Stdout, "Profile: %#v\n", spec)
	desc, err := impl.Describe()
	if err != nil {
		vlog.Errorf("Describe failed: %v", err)
		return err
	}
	fmt.Fprintf(env.Stdout, "Description: %#v\n", desc)
	return nil
}
