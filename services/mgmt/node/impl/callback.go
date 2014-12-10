package impl

import (
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/mgmt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/services/mgmt/node"
)

// InvokeCallback provides the parent device manager with the given name (which
// is expected to be this device manager's object name).
func InvokeCallback(ctx context.T, name string) {
	handle, err := exec.GetChildHandle()
	switch err {
	case nil:
		// Device manager was started by self-update, notify the parent.
		callbackName, err := handle.Config.Get(mgmt.ParentNameConfigKey)
		if err != nil {
			// Device manager was not started by self-update, return silently.
			return
		}
		nmClient := node.ConfigClient(callbackName)
		ctx, cancel := ctx.WithTimeout(ipcContextTimeout)
		defer cancel()
		if err := nmClient.Set(ctx, mgmt.ChildNameConfigKey, name); err != nil {
			vlog.Fatalf("Set(%v, %v) failed: %v", mgmt.ChildNameConfigKey, name, err)
		}
	case exec.ErrNoVersion:
	default:
		vlog.Fatalf("GetChildHandle() failed: %v", err)
	}
}
