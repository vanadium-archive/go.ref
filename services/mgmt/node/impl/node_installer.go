package impl

import (
	"fmt"
	"os"
	"path/filepath"

	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/testutil/blackbox" // For VeyronEnvironment, see TODO.
	"veyron.io/veyron/veyron/services/mgmt/node/config"
)

// InstallFrom takes a veyron object name denoting an application service where
// a node manager application envelope can be obtained.  It downloads the latest
// version of the node manager and installs it.
func InstallFrom(origin string) error {
	// TODO(caprita): Implement.
	return nil
}

// SelfInstall installs the node manager and configures it using the environment
// and the supplied command-line flags.
func SelfInstall(args []string) error {
	configState, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}
	vlog.VI(1).Infof("Config for node manager: %v", configState)
	configState.Name = "dummy" // Just so that Validate passes.
	if err := configState.Validate(); err != nil {
		return fmt.Errorf("invalid config %v: %v", configState, err)
	}

	// Create node manager directory tree.
	nmDir := filepath.Join(configState.Root, "node-manager", "base")
	if err := os.RemoveAll(nmDir); err != nil {
		return fmt.Errorf("RemoveAll(%v) failed: %v", nmDir, err)
	}
	perm := os.FileMode(0700)
	if err := os.MkdirAll(nmDir, perm); err != nil {
		return fmt.Errorf("MkdirAll(%v, %v) failed: %v", nmDir, perm, err)
	}
	envelope := &application.Envelope{
		Args: args,
		// TODO(caprita): Cleaning up env vars to avoid picking up all
		// the garbage from the user's env. Move VeyronEnvironment
		// outside of blackbox if we stick with this strategy.
		// Alternatively, pass the env vars meant specifically for the
		// node manager in a different way.
		Env: blackbox.VeyronEnvironment(os.Environ()),
	}
	if err := linkSelf(nmDir, "noded"); err != nil {
		return err
	}
	// We don't pass in the config state setting, since they're already
	// contained in the environment.
	if err := generateScript(nmDir, nil, envelope); err != nil {
		return err
	}

	// TODO(caprita): Test the node manager we just installed.

	return updateLink(filepath.Join(nmDir, "noded.sh"), configState.CurrentLink)
	// TODO(caprita): Update system management daemon.
}
