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

// shouldDeleteInstance returns true if the tidying policy holds
// that the instance should be deleted.
func shouldDeleteInstance(idir string) (bool, error) {
	fi, err := os.Stat(filepath.Join(idir, device.InstanceStateDeleted.String()))
	if err != nil {
		return false, err
	}

	if fi.ModTime().Add(TidyOlderThan).Before(time.Now()) {
		return true, nil
	}

	return false, nil
}

func pruneDeletedInstances(ctx *context.T, root string) error {
	paths, err := filepath.Glob(filepath.Join(root, "app*", "installation*", "instances", "instance*"))
	if err != nil {
		return err
	}

	type pthError struct {
		pth string
		err error
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

	if len(allerrors) > 0 {
		errormessages := make([]string, 0, len(allerrors))
		for _, ep := range allerrors {
			errormessages = append(errormessages, fmt.Sprintf("path: %s failed: %v", ep.pth, ep.err))
		}
		return verror.New(ErrOperationFailed, ctx, "Some older instances could not be deleted: %s", strings.Join(errormessages, ", "))
	}
	return nil
}
