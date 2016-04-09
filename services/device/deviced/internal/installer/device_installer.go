// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package installer contains logic responsible for managing the device manager
// server, including setting it up / tearing it down, and starting / stopping
// it.
package installer

// When setting up the device manager installation, the installer creates a
// directory structure from which the device manager can be run.  It sets up:
//
// <installDir>                - provided when installer is invoked
//   dmroot/                   - created/owned by the installation
//     device-manager/         - will be the root for the device manager server;
//                               set as <Config.Root> (see comment in
//                               device_service.go for what goes under here)
//       info                  - json-encoded info about the running device manager (currently just the pid)
//       base/                 - initial installation of device manager
//         deviced             - link to deviced (self)
//         deviced.sh          - script to start the device manager
//       device-data/
//         persistent-args     - list of persistent arguments for the device
//                               manager (json encoded)
//       logs/                 - device manager logs will go here
//     current                 - set as <Config.CurrentLink>
//     creation_info           - json-encoded info about the binary that created the directory tree
//     agent_deviced.sh        - script to launch device manager under agent
//     security/               - security agent keeps credentials here
//       keys/
//       principal/
//     agent_logs/             - security agent logs
//       STDERR-<timestamp>
//       STDOUT-<timestamp>
//     service_description     - json-encoded sysinit device manager config
//     inithelper              - soft link to init helper
//
// TODO: we should probably standardize on '-' vs '_' for multi-word filename separators. Note any change
// in the name of creation_info will require some care to ensure the version check continues to work.

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/application"
	"v.io/x/ref"
	"v.io/x/ref/lib/security"
	"v.io/x/ref/services/device/deviced/internal/impl"
	"v.io/x/ref/services/device/deviced/internal/versioning"
	"v.io/x/ref/services/device/internal/config"
	"v.io/x/ref/services/device/internal/sysinit"
)

// restartExitCode is the exit code that the device manager should return when it
// wants to be restarted by its parent (i.e., the security agent).
// This number is picked quasi-arbitrarily from the set of
// exit codes without prior special meanings.
const restartExitCode = 140

// dmRoot is the directory name where the device manager installs itself.
const dmRoot = "dmroot"

// InstallFrom takes a vanadium object name denoting an application service where
// a device manager application envelope can be obtained.  It downloads the
// latest version of the device manager and installs it.
func InstallFrom(origin string) error {
	// TODO(caprita): Implement.
	return nil
}

// initCommand verifies if init mode is enabled, and if so executes the
// appropriate sysinit command.  Returns whether init mode was detected, as well
// as any error encountered.
func initCommand(root, command string, stderr, stdout io.Writer) (bool, error) {
	sdFile := filepath.Join(root, "service_description")
	if _, err := os.Stat(sdFile); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("Stat(%v) failed: %v", sdFile, err)
	}
	helperLink := filepath.Join(root, "inithelper")
	cmd := exec.Command(helperLink, fmt.Sprintf("--service_description=%s", sdFile), command)
	if stderr != nil {
		cmd.Stderr = stderr
	}
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if err := cmd.Run(); err != nil {
		return true, fmt.Errorf("Running init helper %v failed: %v", command, err)
	}
	return true, nil
}

// SelfInstall installs the device manager and configures it using the
// environment and the supplied command-line flags.
func SelfInstall(ctx *context.T, installDir, suidHelper, agent, initHelper, origin string, singleUser, sessionMode, init bool, args, env []string, stderr, stdout io.Writer) error {
	if os.Getenv(ref.EnvCredentials) != "" {
		return fmt.Errorf("Attempting to install device manager under agent with the %q environment variable set.", ref.EnvCredentials)
	}
	root := filepath.Join(installDir, dmRoot)
	if _, err := os.Stat(root); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("%v already exists", root)
	}
	deviceDir := filepath.Join(root, "device-manager", "base")
	perm := os.FileMode(0711)
	if err := os.MkdirAll(deviceDir, perm); err != nil {
		return fmt.Errorf("MkdirAll(%v, %v) failed: %v", deviceDir, perm, err)
	}

	// save info about the binary creating this tree
	if err := versioning.SaveCreatorInfo(ctx, root); err != nil {
		return err
	}

	currLink := filepath.Join(root, "current")
	configState := &config.State{
		Name:        "dummy", // So that Validate passes.
		Root:        root,
		Origin:      origin,
		CurrentLink: currLink,
		Helper:      suidHelper,
	}
	if err := configState.Validate(); err != nil {
		return fmt.Errorf("invalid config %v: %v", configState, err)
	}
	var extraArgs []string
	if name, err := os.Hostname(); err == nil {
		extraArgs = append(extraArgs, fmt.Sprintf("--name=%q", naming.Join("devices", name)))
	}
	if !sessionMode {
		extraArgs = append(extraArgs, fmt.Sprintf("--restart-exit-code=%d", restartExitCode))
	}
	envelope := &application.Envelope{
		Args: append(extraArgs, args...),
		// TODO(caprita): Cleaning up env vars to avoid picking up all
		// the garbage from the user's env.
		// Alternatively, pass the env vars meant specifically for the
		// device manager in a different way.
		Env: impl.VanadiumEnvironment(env),
	}
	if err := impl.SavePersistentArgs(root, envelope.Args); err != nil {
		return err
	}
	if err := impl.LinkSelf(deviceDir, "deviced"); err != nil {
		return err
	}
	configSettings, err := configState.Save(nil)
	if err != nil {
		return fmt.Errorf("failed to serialize config %v: %v", configState, err)
	}
	logs := filepath.Join(root, "device-manager", "logs")
	if err := impl.GenerateScript(deviceDir, configSettings, envelope, logs); err != nil {
		return err
	}

	// TODO(caprita): Test the device manager we just installed.
	if err := impl.UpdateLink(filepath.Join(deviceDir, "deviced.sh"), currLink); err != nil {
		return err
	}

	if err := generateAgentScript(root, agent, currLink, singleUser, sessionMode); err != nil {
		return err
	}
	if init {
		agentScript := filepath.Join(root, "agent_deviced.sh")
		currentUser, err := user.Current()
		if err != nil {
			return err
		}
		sd := &sysinit.ServiceDescription{
			Service:     "deviced",
			Description: "Vanadium Device Manager",
			Binary:      agentScript,
			Command:     []string{agentScript},
			User:        currentUser.Username,
		}
		sdFile := filepath.Join(root, "service_description")
		if err := sd.SaveTo(sdFile); err != nil {
			return fmt.Errorf("SaveTo for %v failed: %v", sd, err)
		}
		helperLink := filepath.Join(root, "inithelper")
		if err := os.Symlink(initHelper, helperLink); err != nil {
			return fmt.Errorf("Symlink(%v, %v) failed: %v", initHelper, helperLink, err)
		}
		if initMode, err := initCommand(root, "install", stderr, stdout); err != nil {
			return err
		} else if !initMode {
			return fmt.Errorf("enabling init mode failed")
		}
	}
	return nil
}

func generateAgentScript(workspace, agent, currLink string, singleUser, sessionMode bool) error {
	securityDir := filepath.Join(workspace, "security")
	principalDir := filepath.Join(securityDir, "principal")
	keyDir := filepath.Join(securityDir, "keys")
	perm := os.FileMode(0700)
	if _, err := security.CreatePersistentPrincipal(principalDir, nil); err != nil {
		return fmt.Errorf("CreatePersistentPrincipal(%v, nil) failed: %v", principalDir, err)
	}
	if err := os.MkdirAll(keyDir, perm); err != nil {
		return fmt.Errorf("MkdirAll(%v, %v) failed: %v", keyDir, perm, err)
	}
	logs := filepath.Join(workspace, "agent_logs")
	if err := os.MkdirAll(logs, perm); err != nil {
		return fmt.Errorf("MkdirAll(%v, %v) failed: %v", logs, perm, err)
	}
	stdoutLog, stderrLog := filepath.Join(logs, "STDOUT"), filepath.Join(logs, "STDERR")
	// TODO(caprita): Switch all our generated bash scripts to use templates.
	output := "#!" + impl.ShellPath + "\n"
	output += "if [ -z \"$DEVICE_MANAGER_DONT_REDIRECT_STDOUT_STDERR\" ]; then\n"
	output += fmt.Sprintf("  TIMESTAMP=$(%s)\n", impl.DateCommand)
	output += fmt.Sprintf("  exec > %s-$TIMESTAMP 2> %s-$TIMESTAMP\n", stdoutLog, stderrLog)
	output += "fi\n"
	output += fmt.Sprintf("%s=%q ", ref.EnvCredentials, principalDir)
	// Escape the path to the binary; %q uses Go-syntax escaping, but it's
	// close enough to Bash that we're using it as an approximation.
	//
	// TODO(caprita/rthellend): expose and use shellEscape (from
	// v.io/x/ref/services/debug/debug/impl.go) instead.
	output += fmt.Sprintf("exec %q --log_dir=%q ", agent, logs)
	if singleUser {
		output += "--with-passphrase=false "
	}
	if !sessionMode {
		output += fmt.Sprintf("--restart-exit-code=!0 ")
	}
	output += fmt.Sprintf("--additional-principals=%q %q", keyDir, currLink)
	path := filepath.Join(workspace, "agent_deviced.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0700); err != nil {
		return fmt.Errorf("WriteFile(%v) failed: %v", path, err)
	}
	// TODO(caprita): Put logs under dmroot/device-manager/logs.
	return nil
}

// Uninstall undoes SelfInstall, removing the device manager's installation
// directory.
func Uninstall(ctx *context.T, installDir, helperPath string, stdout, stderr io.Writer) error {
	// TODO(caprita): ensure device is stopped?

	root := filepath.Join(installDir, dmRoot)
	if _, err := initCommand(root, "uninstall", stdout, stderr); err != nil {
		return err
	}
	impl.InitSuidHelper(ctx, helperPath)
	return impl.DeleteFileTree(ctx, root, stdout, stderr)
}

// Start starts the device manager.
func Start(ctx *context.T, installDir string, stderr, stdout io.Writer) error {
	// TODO(caprita): make sure it's not already running?

	root := filepath.Join(installDir, dmRoot)

	if initMode, err := initCommand(root, "start", stderr, stdout); err != nil {
		return err
	} else if initMode {
		return nil
	}

	if os.Getenv(ref.EnvCredentials) != "" {
		return fmt.Errorf("Attempting to run device manager under agent with the %q environment variable set.", ref.EnvCredentials)
	}
	agentScript := filepath.Join(root, "agent_deviced.sh")
	cmd := exec.Command(agentScript)
	if stderr != nil {
		cmd.Stderr = stderr
	}
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Start failed: %v", err)
	}

	// Save away the agent's pid to be used for stopping later ...
	if cmd.Process.Pid == 0 {
		fmt.Fprintf(stderr, "Unable to get a pid for successfully-started agent!")
		return nil // We tolerate the error, at the expense of being able to stop later
	}
	mi := &impl.ManagerInfo{
		Pid: cmd.Process.Pid,
	}
	if err := impl.SaveManagerInfo(filepath.Join(root, "agent-deviced"), mi); err != nil {
		return fmt.Errorf("failed to save info for agent-deviced: %v", err)
	}

	return nil
}

// Stop stops the device manager.
func Stop(ctx *context.T, installDir string, stderr, stdout io.Writer) error {
	root := filepath.Join(installDir, dmRoot)
	if initMode, err := initCommand(root, "stop", stderr, stdout); err != nil {
		return err
	} else if initMode {
		return nil
	}

	agentPid, devmgrPid := 0, 0

	// Load the agent pid
	info, err := impl.LoadManagerInfo(filepath.Join(root, "agent-deviced"))
	if err != nil {
		return fmt.Errorf("loadManagerInfo failed for agent-deviced: %v", err)
	}
	if syscall.Kill(info.Pid, 0) == nil { // Save the pid if it's currently live
		agentPid = info.Pid
	}

	// Load the device manager pid
	info, err = impl.LoadManagerInfo(filepath.Join(root, "device-manager"))
	if err != nil {
		return fmt.Errorf("loadManagerInfo failed for device-manager: %v", err)
	}
	if syscall.Kill(info.Pid, 0) == nil { // Save the pid if it's currently live
		devmgrPid = info.Pid
	}

	if agentPid == 0 && devmgrPid == 0 {
		return fmt.Errorf("stop could not find any live pids to stop")
	}

	// Set up waiters for each nonzero pid. This ensures that exiting processes are reaped when
	// the agent or device manager happen to be children of this process. (Not commonly the case,
	// but it does occur in the impl test.)
	if agentPid != 0 {
		go func() {
			if p, err := os.FindProcess(agentPid); err == nil {
				p.Wait()
			}
		}()
	}
	if devmgrPid != 0 {
		go func() {
			if p, err := os.FindProcess(devmgrPid); err == nil {
				p.Wait()
			}
		}()
	}

	// First, send SIGINT to the agent. We expect both the agent and the device manager to
	// exit as a result within 15 seconds
	if agentPid != 0 {
		if err = syscall.Kill(agentPid, syscall.SIGINT); err != nil {
			return fmt.Errorf("sending SIGINT to %d: %v", agentPid, err)
		}
		for i := 0; i < 30 && syscall.Kill(agentPid, 0) == nil; i++ {
			time.Sleep(500 * time.Millisecond)
			if i%5 == 4 {
				fmt.Fprintf(stderr, "waiting for agent (pid %d) to die...\n", agentPid)
			}
		}
		if syscall.Kill(agentPid, 0) == nil { // agent is still alive, resort to brute force
			fmt.Fprintf(stderr, "sending SIGKILL to agent %d\n", agentPid)
			if err = syscall.Kill(agentPid, syscall.SIGKILL); err != nil {
				fmt.Fprintf(stderr, "Sending SIGKILL to %d: %v\n", agentPid, err)
				// not returning here, so that we check & kill the device manager too
			}
		}
	}

	// If the device manager is still alive, forcibly kill it
	if syscall.Kill(devmgrPid, 0) == nil {
		fmt.Fprintf(stderr, "sending SIGKILL to device manager %d\n", devmgrPid)
		if err = syscall.Kill(devmgrPid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("sending SIGKILL to device manager %d: %v", devmgrPid, err)
		}
	}

	// By now, nothing should be alive. Check and report
	if agentPid != 0 && syscall.Kill(agentPid, 0) == nil {
		return fmt.Errorf("multiple attempts to kill agent pid %d have failed", agentPid)
	}
	if devmgrPid != 0 && syscall.Kill(devmgrPid, 0) == nil {
		return fmt.Errorf("multiple attempts to kill device manager pid %d have failed", devmgrPid)
	}

	// Should we remove the agentd and deviced info files here? Not removing them
	// increases the chances that we later rerun stop and shoot some random process. Removing
	// them makes it impossible to run stop a second time (although that shouldn't be necessary)
	// and also introduces the potential for a race condition if a new agent/deviced are started
	// right after these ones get killed.
	//
	// TODO: Reconsider this when we add stronger protection to make sure that the pids being
	// signalled are in fact the agent and/or device manager

	// Process was killed succesfully
	return nil
}
