// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/services/device"
	"v.io/v23/verror"
)

// This file contains the various routines that the device manager uses
// to tidy up its persisted but no longer necessary state.

// TidyAge defaults to 1 day. Settable for tests.
var TidyOlderThan = time.Hour * 24

func shouldDelete(idir, suffix string) (bool, error) {
	fi, err := os.Stat(filepath.Join(idir, suffix))
	if err != nil {
		return false, err
	}

	if fi.ModTime().Add(TidyOlderThan).Before(time.Now()) {
		return true, nil
	}

	return false, nil
}

// shouldDeleteInstallation returns true if the tidying policy holds
// for this installation.
func shouldDeleteInstallation(idir string) (bool, error) {
	return shouldDelete(idir, device.InstallationStateUninstalled.String())
}

// shouldDeleteInstance returns true if the tidying policy holds
// that the instance should be deleted.
func shouldDeleteInstance(idir string) (bool, error) {
	return shouldDelete(idir, device.InstanceStateDeleted.String())
}

type pthError struct {
	pth string
	err error
}

func pruneDeletedInstances(ctx *context.T, root string) error {
	paths, err := filepath.Glob(filepath.Join(root, "app*", "installation*", "instances", "instance*"))
	if err != nil {
		return err
	}

	allerrors := make([]pthError, 0)

	for _, pth := range paths {
		state, err := getInstanceState(pth)
		if err != nil {
			allerrors = append(allerrors, pthError{pth, err})
			continue
		}
		if state != device.InstanceStateDeleted {
			continue
		}

		shouldDelete, err := shouldDeleteInstance(pth)
		if err != nil {
			allerrors = append(allerrors, pthError{pth, err})
			continue
		}

		if shouldDelete {
			if err := suidHelper.deleteFileTree(pth, nil, nil); err != nil {
				allerrors = append(allerrors, pthError{pth, err})
			}
		}
	}
	return processErrors(ctx, allerrors)
}

func processErrors(ctx *context.T, allerrors []pthError) error {
	if len(allerrors) > 0 {
		errormessages := make([]string, 0, len(allerrors))
		for _, ep := range allerrors {
			errormessages = append(errormessages, fmt.Sprintf("path: %s failed: %v", ep.pth, ep.err))
		}
		return verror.New(ErrOperationFailed, ctx, "Some older instances could not be deleted: %s", strings.Join(errormessages, ", "))
	}
	return nil
}

func pruneUninstalledInstallations(ctx *context.T, root string) error {
	// Read all the Uninstalled installations into a map.
	installationPaths, err := filepath.Glob(filepath.Join(root, "app*", "installation*"))
	if err != nil {
		return err
	}
	pruneCandidates := make(map[string]struct{}, len(installationPaths))
	for _, p := range installationPaths {
		state, err := getInstallationState(p)
		if err != nil {
			return err
		}

		if state != device.InstallationStateUninstalled {
			continue
		}

		pruneCandidates[p] = struct{}{}
	}

	instancePaths, err := filepath.Glob(filepath.Join(root, "app*", "installation*", "instances", "instance*", "installation"))
	if err != nil {
		return err
	}

	allerrors := make([]pthError, 0)

	// Filter out installations that are still owned by an instance. Note
	// that pruneUninstalledInstallations runs after
	// pruneDeletedInstances so that freshly-pruned Instances will not
	// retain the Installation.
	for _, idir := range instancePaths {
		installPath, err := os.Readlink(idir)
		if err != nil {
			allerrors = append(allerrors, pthError{idir, err})
			continue
		}

		if _, ok := pruneCandidates[installPath]; ok {
			delete(pruneCandidates, installPath)
		}
	}

	// All remaining entries in pruneCandidates are not referenced by
	// any instance.
	for pth, _ := range pruneCandidates {
		shouldDelete, err := shouldDeleteInstallation(pth)
		if err != nil {
			allerrors = append(allerrors, pthError{pth, err})
			continue
		}

		if shouldDelete {
			if err := suidHelper.deleteFileTree(pth, nil, nil); err != nil {
				allerrors = append(allerrors, pthError{pth, err})
			}
		}
	}
	return processErrors(ctx, allerrors)
}
