package impl

import (
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/services/mgmt/device"
)

// InvokeCallback provides the parent device manager with the given name (which
// is expected to be this device manager's object name).
func InvokeCallback(ctx *context.T, name string) {
	handle, err := exec.GetChildHandle()
	switch err {
	case nil:
		// Device manager was started by self-update, notify the parent.
		callbackName, err := handle.Config.Get(mgmt.ParentNameConfigKey)
		if err != nil {
			// Device manager was not started by self-update, return silently.
			return
		}
		client := device.ConfigClient(callbackName)
		ctx, cancel := context.WithTimeout(ctx, ipcContextTimeout)
		defer cancel()
		if err := client.Set(ctx, mgmt.ChildNameConfigKey, name); err != nil {
			vlog.Fatalf("Set(%v, %v) failed: %v", mgmt.ChildNameConfigKey, name, err)
		}
	case exec.ErrNoVersion:
	default:
		vlog.Fatalf("GetChildHandle() failed: %v", err)
	}
}
