// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"v.io/v23/context"
	"v.io/v23/mgmt"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/exec"
	"v.io/x/ref/services/mgmt/device"
)

// InvokeCallback provides the parent device manager with the given name (which
// is expected to be this device manager's object name).
func InvokeCallback(ctx *context.T, name string) {
	handle, err := exec.GetChildHandle()
	if err == nil {
		// Device manager was started by self-update, notify the parent.
		callbackName, err := handle.Config.Get(mgmt.ParentNameConfigKey)
		if err != nil {
			// Device manager was not started by self-update, return silently.
			return
		}
		client := device.ConfigClient(callbackName)
		ctx, cancel := context.WithTimeout(ctx, rpcContextTimeout)
		defer cancel()
		if err := client.Set(ctx, mgmt.ChildNameConfigKey, name); err != nil {
			vlog.Fatalf("Set(%v, %v) failed: %v", mgmt.ChildNameConfigKey, name, err)
		}
	} else if verror.ErrorID(err) != exec.ErrNoVersion.ID {
		vlog.Fatalf("GetChildHandle() failed: %v", err)
	}
}
