package impl

// The device installer logic is responsible for managing the device manager
// server, including setting it up / tearing it down, and starting / stopping
// it.
//
// When setting up the device manager installation, the installer creates a
// directory structure from which the device manager can be run.  It sets up:
//
// <installDir>                - provided when installer is invoked
//   dmroot/                   - created/owned by the installation
//     device-manager/         - will be the root for the device manager server;
//                               set as <Config.Root> (see comment in
//                               device_service.go for what goes under here)
//       base/                 - initial installation of device manager
//         deviced             - link to deviced (self)
//         deviced.sh          - script to start the device manager
//       device-data/
//         persistent-args     - list of persistent arguments for the device
//                               manager (json encoded)
//       logs/                 - device manager logs will go here
//     current                 - set as <Config.CurrentLink>
//     agent_deviced.sh        - script to launch device manager under agent
//     security/               - security agent keeps credentials here
//       keys/
//       principal/
//     agent_logs/             - security agent logs
//       STDERR-<timestamp>
//       STDOUT-<timestamp>
//     service_description     - json-encoded sysinit device manager config
//     inithelper              - soft link to init helper

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"

	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/sysinit"
)

// restartExitCode is the exit code that the device manager should return when it
// wants to be restarted by its parent (i.e., the security agent).
// This number is picked quasi-arbitrarily from the set of
// exit codes without prior special meanings.
const restartExitCode = 140

// dmRoot is the directory name where the device manager installs itself.
const dmRoot = "dmroot"

// InstallFrom takes a veyron object name denoting an application service where
// a device manager application envelope can be obtained.  It downloads the
// latest version of the device manager and installs it.
func InstallFrom(origin string) error {
	// TODO(caprita): Implement.
	return nil
}

var allowedVarsRE = regexp.MustCompile("VEYRON_.*|NAMESPACE_ROOT.*|PAUSE_BEFORE_STOP|TMPDIR")

var deniedVarsRE = regexp.MustCompile("VEYRON_EXEC_VERSION")

// filterEnvironment returns only the environment variables, specified by
// the env parameter, whose names match the supplied regexp.
func filterEnvironment(env []string, allow, deny *regexp.Regexp) []string {
	var ret []string
	for _, e := range env {
		if eqIdx := strings.Index(e, "="); eqIdx > 0 {
			key := e[:eqIdx]
			if deny.MatchString(key) {
				continue
			}
			if allow.MatchString(key) {
				ret = append(ret, e)
			}
		}
	}
	return ret
}

// VeyronEnvironment returns only the environment variables that are specific
// to the Veyron system.
func VeyronEnvironment(env []string) []string {
	return filterEnvironment(env, allowedVarsRE, deniedVarsRE)
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
func SelfInstall(installDir, suidHelper, agent, initHelper, origin string, singleUser, sessionMode, init bool, args, env []string, stderr, stdout io.Writer) error {
	root := filepath.Join(installDir, dmRoot)
	if _, err := os.Stat(root); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("%v already exists", root)
	}
	deviceDir := filepath.Join(root, "device-manager", "base")
	perm := os.FileMode(0700)
	if err := os.MkdirAll(deviceDir, perm); err != nil {
		return fmt.Errorf("MkdirAll(%v, %v) failed: %v", deviceDir, perm, err)
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
		extraArgs = append(extraArgs, fmt.Sprintf("--restart_exit_code=%d", restartExitCode))
	}
	envelope := &application.Envelope{
		Args: append(extraArgs, args...),
		// TODO(caprita): Cleaning up env vars to avoid picking up all
		// the garbage from the user's env.
		// Alternatively, pass the env vars meant specifically for the
		// device manager in a different way.
		Env: VeyronEnvironment(env),
	}
	if err := savePersistentArgs(root, envelope.Args); err != nil {
		return err
	}
	if err := linkSelf(deviceDir, "deviced"); err != nil {
		return err
	}
	configSettings, err := configState.Save(nil)
	if err != nil {
		return fmt.Errorf("failed to serialize config %v: %v", configState, err)
	}
	logs := filepath.Join(root, "device-manager", "logs")
	if err := generateScript(deviceDir, configSettings, envelope, logs); err != nil {
		return err
	}

	// TODO(caprita): Test the device manager we just installed.
	if err := updateLink(filepath.Join(deviceDir, "deviced.sh"), currLink); err != nil {
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
	if err := os.MkdirAll(principalDir, perm); err != nil {
		return fmt.Errorf("MkdirAll(%v, %v) failed: %v", principalDir, perm, err)
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
	output := "#!/bin/bash\n"
	output += "if [ -z \"$DEVICE_MANAGER_DONT_REDIRECT_STDOUT_STDERR\" ]; then\n"
	output += fmt.Sprintf("  TIMESTAMP=$(%s)\n", dateCommand)
	output += fmt.Sprintf("  exec > %s-$TIMESTAMP 2> %s-$TIMESTAMP\n", stdoutLog, stderrLog)
	output += "fi\n"
	output += fmt.Sprintf("%s=%q ", consts.VeyronCredentials, principalDir)
	// Escape the path to the binary; %q uses Go-syntax escaping, but it's
	// close enough to Bash that we're using it as an approximation.
	//
	// TODO(caprita/rthellend): expose and use shellEscape (from
	// veyron/tools/debug/impl.go) instead.
	output += fmt.Sprintf("exec %q --log_dir=%q ", agent, logs)
	if singleUser {
		output += "--no_passphrase "
	}
	if !sessionMode {
		output += fmt.Sprintf("--restart_exit_code=!0 ")
	}
	output += fmt.Sprintf("--additional_principals=%q %q", keyDir, currLink)
	path := filepath.Join(workspace, "agent_deviced.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0700); err != nil {
		return fmt.Errorf("WriteFile(%v) failed: %v", path, err)
	}
	// TODO(caprita): Put logs under dmroot/device-manager/logs.
	return nil
}

// Uninstall undoes SelfInstall, removing the device manager's installation
// directory.
func Uninstall(installDir string, stdout, stderr io.Writer) error {
	// TODO(caprita): ensure device is stopped?

	root := filepath.Join(installDir, dmRoot)
	if _, err := initCommand(root, "uninstall", stdout, stderr); err != nil {
		return err
	}
	// TODO(caprita): Use suidhelper to delete dirs/files owned by other
	// users under the app work dirs.
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("RemoveAll(%v) failed: %v", root, err)
	}
	return nil
}

// Start starts the device manager.
func Start(installDir string, stderr, stdout io.Writer) error {
	// TODO(caprita): make sure it's not already running?

	root := filepath.Join(installDir, dmRoot)

	if initMode, err := initCommand(root, "start", stderr, stdout); err != nil {
		return err
	} else if initMode {
		return nil
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
	info, err := loadManagerInfo(filepath.Join(root, "device-manager"))
	if err != nil {
		return fmt.Errorf("loadManagerInfo failed: %v", err)
	}
	if err := device.ApplicationClient(info.MgrName).Stop(ctx, 5); err != nil {
		return fmt.Errorf("Stop failed: %v", err)
	}
	// TODO(caprita): Wait for the (device|agent) process to be gone.
	return nil
}
