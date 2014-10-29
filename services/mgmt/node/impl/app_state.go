package impl

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"veyron.io/veyron/veyron2/vlog"
)

// installationState describes the states that an installation can be in at any
// time.
type installationState int

const (
	active installationState = iota
	uninstalled
)

// String returns the name that will be used to encode the state as a file name
// in the installation's dir.
func (s installationState) String() string {
	switch s {
	case active:
		return "active"
	case uninstalled:
		return "uninstalled"
	default:
		return "unknown"
	}
}

func installationStateIs(installationDir string, state installationState) bool {
	if _, err := os.Stat(filepath.Join(installationDir, state.String())); err != nil {
		return false
	}
	return true
}

func transitionInstallation(installationDir string, initial, target installationState) error {
	return transitionState(installationDir, initial, target)
}

func initializeInstallation(installationDir string, initial installationState) error {
	return initializeState(installationDir, initial)
}

// instanceState describes the states that an instance can be in at any time.
type instanceState int

const (
	starting instanceState = iota
	started
	suspending
	suspended
	stopping
	stopped
)

// String returns the name that will be used to encode the state as a file name
// in the instance's dir.
func (s instanceState) String() string {
	switch s {
	case starting:
		return "starting"
	case started:
		return "started"
	case suspending:
		return "suspending"
	case suspended:
		return "suspended"
	case stopping:
		return "stopping"
	case stopped:
		return "stopped"
	default:
		return "unknown"
	}
}

func instanceStateIs(instanceDir string, state instanceState) bool {
	if _, err := os.Stat(filepath.Join(instanceDir, state.String())); err != nil {
		return false
	}
	return true
}
func transitionInstance(instanceDir string, initial, target instanceState) error {
	return transitionState(instanceDir, initial, target)
}

func initializeInstance(instanceDir string, initial instanceState) error {
	return initializeState(instanceDir, initial)
}

func transitionState(dir string, initial, target fmt.Stringer) error {
	initialState := filepath.Join(dir, initial.String())
	targetState := filepath.Join(dir, target.String())
	if err := os.Rename(initialState, targetState); err != nil {
		if os.IsNotExist(err) {
			return errInvalidOperation
		}
		vlog.Errorf("Rename(%v, %v) failed: %v", initialState, targetState, err) // Something went really wrong.
		return errOperationFailed
	}
	return nil
}

func initializeState(dir string, initial fmt.Stringer) error {
	initialStatus := filepath.Join(dir, initial.String())
	if err := ioutil.WriteFile(initialStatus, []byte("status"), 0600); err != nil {
		vlog.Errorf("WriteFile(%v) failed: %v", initialStatus, err)
		return errOperationFailed
	}
	return nil
}
