package impl

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"

	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/services/mgmt/device/config"
)

// stopExitCode is the exit code that the device manager should return when it
// gets a Stop RPC.  This number is picked quasi-arbitrarily from the set of
// exit codes without prior special meanings.
const stopExitCode = 140

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

// SelfInstall installs the device manager and configures it using the
// environment and the supplied command-line flags.
func SelfInstall(installDir, suidHelper, agent string, singleUser, sessionMode bool, args, env []string) error {
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
		CurrentLink: currLink,
		Helper:      suidHelper,
	}
	if err := configState.Validate(); err != nil {
		return fmt.Errorf("invalid config %v: %v", configState, err)
	}
	var extraArgs []string
	if name, err := os.Hostname(); err == nil {
		extraArgs = append(extraArgs, fmt.Sprintf("--name=%q", name))
	}
	if !sessionMode {
		extraArgs = append(extraArgs, fmt.Sprintf("--stop_exit_code=%d", stopExitCode))
	}
	envelope := &application.Envelope{
		Args: append(extraArgs, args...),
		// TODO(caprita): Cleaning up env vars to avoid picking up all
		// the garbage from the user's env.
		// Alternatively, pass the env vars meant specifically for the
		// device manager in a different way.
		Env: VeyronEnvironment(env),
	}
	if err := linkSelf(deviceDir, "deviced"); err != nil {
		return err
	}
	configSettings, err := configState.Save(nil)
	if err != nil {
		return fmt.Errorf("failed to serialize config %v: %v", configState, err)
	}
	if err := generateScript(deviceDir, configSettings, envelope); err != nil {
		return err
	}

	// TODO(caprita): Test the device manager we just installed.
	if err := updateLink(filepath.Join(deviceDir, "deviced.sh"), currLink); err != nil {
		return err
	}

	return generateAgentScript(root, agent, currLink, singleUser, sessionMode)
	// TODO(caprita): Update system management daemon.
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
	// TODO(caprita): Switch all our generated bash scripts to use templates.
	output := "#!/bin/bash\n"
	output += fmt.Sprintf("%s=%q ", consts.VeyronCredentials, principalDir)
	// Escape the path to the binary; %q uses Go-syntax escaping, but it's
	// close enough to Bash that we're using it as an approximation.
	//
	// TODO(caprita/rthellend): expose and use shellEscape (from
	// veyron/tools/debug/impl.go) instead.
	output += fmt.Sprintf("exec %q ", agent)
	if singleUser {
		output += "--no_passphrase "
	}
	if !sessionMode {
		output += fmt.Sprintf("--restart_exit_code=!%d ", stopExitCode)
	}
	output += fmt.Sprintf("--additional_principals=%q ", keyDir)
	output += fmt.Sprintf("%q\n", currLink)
	path := filepath.Join(workspace, "agent_deviced.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0700); err != nil {
		return fmt.Errorf("WriteFile(%v) failed: %v", path, err)
	}
	// TODO(caprita): Put logs under dmroot/device-manager/logs.
	return nil
}

// Uninstall undoes SelfInstall, removing the device manager's installation
// directory.
func Uninstall(installDir string) error {
	// TODO(caprita): ensure device is stopped?

	root := filepath.Join(installDir, dmRoot)
	// TODO(caprita): Use suidhelper to delete dirs/files owned by other
	// users under the app work dirs.
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("RemoveAll(%v) failed: %v", root, err)
	}
	// TODO(caprita): Update system management daemon.
	return nil
}

// Start starts the device manager.
func Start(installDir string, stderr, stdout io.Writer) error {
	// TODO(caprita): make sure it's not already running?

	root := filepath.Join(installDir, dmRoot)
	// TODO(caprita): In non-session mode, use system management daemon.
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
func Stop(ctx *context.T, installDir string) error {
	root := filepath.Join(installDir, dmRoot)
	info, err := loadManagerInfo(filepath.Join(root, "device-manager"))
	if err != nil {
		return fmt.Errorf("loadManagerInfo failed: %v", err)
	}
	if err := device.ApplicationClient(info.MgrName).Stop(ctx, 5); err != nil {
		return fmt.Errorf("Stop failed: %v", err)
	}
	// TODO(caprita): Wait for the (device|agent) process to be gone.

	// TODO(caprita): In non-session mode, use system management daemon.
	return nil
}
