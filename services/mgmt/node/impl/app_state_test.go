package impl

import (
	"io/ioutil"
	"os"
	"testing"
)

// TestInstallationState verifies the state transition logic for app installations.
func TestInstallationState(t *testing.T) {
	dir, err := ioutil.TempDir("", "installation")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	// Uninitialized state.
	if transitionInstallation(dir, active, uninstalled) == nil {
		t.Fatalf("transitionInstallation should have failed")
	}
	if isActive, isUninstalled := installationStateIs(dir, active), installationStateIs(dir, uninstalled); isActive || isUninstalled {
		t.Fatalf("isActive, isUninstalled = %t, %t (expected false, false)", isActive, isUninstalled)
	}
	// Initialize.
	if err := initializeInstallation(dir, active); err != nil {
		t.Fatalf("initializeInstallation failed: %v", err)
	}
	if !installationStateIs(dir, active) {
		t.Fatalf("Installation state expected to be %v", active)
	}
	if err := transitionInstallation(dir, active, uninstalled); err != nil {
		t.Fatalf("transitionInstallation failed: %v", err)
	}
	if !installationStateIs(dir, uninstalled) {
		t.Fatalf("Installation state expected to be %v", uninstalled)
	}
	// Invalid transition: wrong initial state.
	if transitionInstallation(dir, active, uninstalled) == nil {
		t.Fatalf("transitionInstallation should have failed")
	}
	if !installationStateIs(dir, uninstalled) {
		t.Fatalf("Installation state expected to be %v", uninstalled)
	}
}

// TestInstanceState verifies the state transition logic for app instances.
func TestInstanceState(t *testing.T) {
	dir, err := ioutil.TempDir("", "instance")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	// Uninitialized state.
	if transitionInstance(dir, starting, started) == nil {
		t.Fatalf("transitionInstance should have failed")
	}
	// Initialize.
	if err := initializeInstance(dir, suspending); err != nil {
		t.Fatalf("initializeInstance failed: %v", err)
	}
	if err := transitionInstance(dir, suspending, suspended); err != nil {
		t.Fatalf("transitionInstance failed: %v", err)
	}
	// Invalid transition: wrong initial state.
	if transitionInstance(dir, suspending, suspended) == nil {
		t.Fatalf("transitionInstance should have failed")
	}
	if err := transitionInstance(dir, suspended, stopped); err != nil {
		t.Fatalf("transitionInstance failed: %v", err)
	}
}
