package impl

import (
	"veyron/services/mgmt/lib/exec"
	inode "veyron/services/mgmt/node"

	"veyron2/mgmt"
	"veyron2/rt"
	"veyron2/vlog"
)

// InvokeCallback provides the parent node manager with the given name (which is
// expected to be this node manager's object name).
func InvokeCallback(name string) {
	handle, err := exec.GetChildHandle()
	switch err {
	case nil:
		// Node manager was started by self-update, notify the parent.
		callbackName, err := handle.Config.Get(mgmt.ParentNodeManagerConfigKey)
		if err != nil {
			vlog.Fatalf("Failed to get callback name from config: %v", err)
		}
		nmClient, err := inode.BindNode(callbackName)
		if err != nil {
			vlog.Fatalf("BindNode(%v) failed: %v", callbackName, err)
		}
		if err := nmClient.Set(rt.R().NewContext(), mgmt.ChildNodeManagerConfigKey, name); err != nil {
			vlog.Fatalf("Set(%v, %v) failed: %v", mgmt.ChildNodeManagerConfigKey, name, err)
		}
	case exec.ErrNoVersion:
	default:
		vlog.Fatalf("GetChildHandle() failed: %v", err)
	}
}
