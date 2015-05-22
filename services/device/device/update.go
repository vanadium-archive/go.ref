// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"time"

	"v.io/v23/context"
	"v.io/v23/services/device"
	"v.io/v23/verror"

	"v.io/x/lib/cmdline"
	deviceimpl "v.io/x/ref/services/device/internal/impl"
)

var cmdUpdate = &cmdline.Command{
	Runner:   globRunner(runUpdate),
	Name:     "update",
	Short:    "Update device manager or applications.",
	Long:     "Update the device manager or application instances and installations",
	ArgsName: "<app name patterns...>",
	ArgsLong: `
<app name patterns...> are vanadium object names or glob name patterns corresponding to the device manager service, or to application installations and instances.`,
}

func instanceIsRunning(ctx *context.T, von string) (bool, error) {
	status, err := device.ApplicationClient(von).Status(ctx)
	if err != nil {
		return false, fmt.Errorf("Failed to get status for instance %q: %v", von, err)
	}
	s, ok := status.(device.StatusInstance)
	if !ok {
		return false, fmt.Errorf("Status for instance %q of wrong type (%T)", von, status)
	}
	return s.Value.State == device.InstanceStateRunning, nil
}

func updateInstance(ctx *context.T, stdout, stderr io.Writer, name string, status device.StatusInstance) (retErr error) {
	if status.Value.State == device.InstanceStateRunning {
		if err := device.ApplicationClient(name).Kill(ctx, killDeadline); err != nil {
			// Check the app's state again in case we killed it,
			// nevermind any errors.  The sleep is because Kill
			// currently (4/29/15) returns asynchronously with the
			// device manager shooting the app down.
			time.Sleep(time.Second)
			running, rerr := instanceIsRunning(ctx, name)
			if rerr != nil {
				return rerr
			}
			if running {
				return fmt.Errorf("Kill failed: %v", err)
			}
			fmt.Fprintf(stderr, "WARNING for \"%s\": recovered from Kill error (%s). Proceeding with update.\n", name, err)
		}
		// App was running, and we killed it, so we need to run it again
		// after the update.
		defer func() {
			if err := device.ApplicationClient(name).Run(ctx); err != nil {
				err = fmt.Errorf("Run failed: %v", err)
				if retErr == nil {
					retErr = err
				} else {
					fmt.Fprintf(stderr, "ERROR for \"%s\": %v.\n", name, err)
				}
			}
		}()
	}
	// Update the instance.
	switch err := device.ApplicationClient(name).Update(ctx); {
	case err == nil:
		fmt.Fprintf(stdout, "Successfully updated instance \"%s\".\n", name)
		return nil
	case verror.ErrorID(err) == deviceimpl.ErrUpdateNoOp.ID:
		// TODO(caprita): Ideally, we wouldn't even attempt a kill /
		// restart if there's no newer version of the application.
		fmt.Fprintf(stdout, "Instance \"%s\" already up to date.\n", name)
		return nil
	default:
		return fmt.Errorf("Update failed: %v", err)
	}
}

func updateOne(ctx *context.T, what string, stdout, stderr io.Writer, name string) error {
	switch err := device.ApplicationClient(name).Update(ctx); {
	case err == nil:
		fmt.Fprintf(stdout, "Successfully updated version for %s \"%s\".\n", what, name)
		return nil
	case verror.ErrorID(err) == deviceimpl.ErrUpdateNoOp.ID:
		fmt.Fprintf(stdout, "%s \"%s\" already up to date.\n", what, name)
		return nil
	default:
		return fmt.Errorf("Update failed: %v", err)
	}
}

func runUpdate(entry globResult, ctx *context.T, stdout, stderr io.Writer) error {
	switch entry.kind {
	case applicationInstanceObject:
		return updateInstance(ctx, stdout, stderr, entry.name, entry.status.(device.StatusInstance))
	case applicationInstallationObject:
		return updateOne(ctx, "installation", stdout, stderr, entry.name)
	case deviceServiceObject:
		return updateOne(ctx, "device service", stdout, stderr, entry.name)
	default:
		return fmt.Errorf("unhandled object kind %v", entry.kind)
	}
}
