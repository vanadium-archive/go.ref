package impl

// The node invoker is responsible for managing the state of the node manager
// itself.  The implementation expects that the node manager installations are
// all organized in the following directory structure:
//
// <config.Root>/
//   node-manager/
//     <version 1 timestamp>/  - timestamp of when the version was downloaded
//       noded                 - the node manager binary
//       noded.sh              - a shell script to start the binary
//     <version 2 timestamp>
//     ...
//     node-data/
//       acl.nodemanager
//	 acl.signature
//
// The node manager is always expected to be started through the symbolic link
// passed in as config.CurrentLink, which is monitored by an init daemon. This
// provides for simple and robust updates.
//
// To update the node manager to a newer version, a new workspace is created and
// the symlink is updated to the new noded.sh script. Similarly, to revert the
// node manager to a previous version, all that is required is to update the
// symlink to point to the previous noded.sh script.

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"veyron/lib/config"
	vexec "veyron/services/mgmt/lib/exec"
	iconfig "veyron/services/mgmt/node/config"
	"veyron/services/mgmt/profile"

	"veyron2/context"
	"veyron2/ipc"
	"veyron2/mgmt"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/binary"
	"veyron2/services/mgmt/node"
	"veyron2/vlog"
)

type updatingState struct {
	// updating is a flag that records whether this instance of node
	// manager is being updated.
	updating bool
	// updatingMutex is a lock for coordinating concurrent access to
	// <updating>.
	updatingMutex sync.Mutex
}

func newUpdatingState() *updatingState {
	return new(updatingState)
}

func (u *updatingState) testAndSetUpdating() bool {
	u.updatingMutex.Lock()
	defer u.updatingMutex.Unlock()
	if u.updating {
		return true
	}
	u.updating = true
	return false
}

func (u *updatingState) unsetUpdating() {
	u.updatingMutex.Lock()
	u.updating = false
	u.updatingMutex.Unlock()
}

// nodeInvoker holds the state of a node manager method invocation.
type nodeInvoker struct {
	updating *updatingState
	callback *callbackState
	config   *iconfig.State
	disp     *dispatcher
}

func (i *nodeInvoker) Claim(call ipc.ServerContext) error {
	// Get the blessing to be used by the claimant
	blessing := call.Blessing()
	if blessing == nil {
		return errInvalidBlessing
	}
	return i.disp.claimNodeManager(blessing)
}

func (*nodeInvoker) Describe(ipc.ServerContext) (node.Description, error) {
	empty := node.Description{}
	nodeProfile, err := computeNodeProfile()
	if err != nil {
		return empty, err
	}
	knownProfiles, err := getKnownProfiles()
	if err != nil {
		return empty, err
	}
	result := matchProfiles(nodeProfile, knownProfiles)
	return result, nil
}

func (*nodeInvoker) IsRunnable(_ ipc.ServerContext, description binary.Description) (bool, error) {
	nodeProfile, err := computeNodeProfile()
	if err != nil {
		return false, err
	}
	binaryProfiles := make([]profile.Specification, 0)
	for name, _ := range description.Profiles {
		profile, err := getProfile(name)
		if err != nil {
			return false, err
		}
		binaryProfiles = append(binaryProfiles, *profile)
	}
	result := matchProfiles(nodeProfile, binaryProfiles)
	return len(result.Profiles) > 0, nil
}

func (*nodeInvoker) Reset(call ipc.ServerContext, deadline uint64) error {
	// TODO(jsimsa): Implement.
	return nil
}

// getCurrentFileInfo returns the os.FileInfo for both the symbolic link
// CurrentLink, and the node script in the workspace that this link points to.
func (i *nodeInvoker) getCurrentFileInfo() (os.FileInfo, string, error) {
	path := i.config.CurrentLink
	link, err := os.Lstat(path)
	if err != nil {
		vlog.Errorf("Lstat(%v) failed: %v", path, err)
		return nil, "", err
	}
	scriptPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		vlog.Errorf("EvalSymlinks(%v) failed: %v", path, err)
		return nil, "", err
	}
	return link, scriptPath, nil
}

// TODO(caprita): Move updateLink to util.go now that app_invoker also uses it.
func updateLink(target, link string) error {
	newLink := link + ".new"
	fi, err := os.Lstat(newLink)
	if err == nil {
		if err := os.Remove(fi.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", fi.Name(), err)
			return errOperationFailed
		}
	}
	if err := os.Symlink(target, newLink); err != nil {
		vlog.Errorf("Symlink(%v, %v) failed: %v", target, newLink, err)
		return errOperationFailed
	}
	if err := os.Rename(newLink, link); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", newLink, link, err)
		return errOperationFailed
	}
	return nil
}

func (i *nodeInvoker) revertNodeManager() error {
	if err := updateLink(i.config.Previous, i.config.CurrentLink); err != nil {
		return err
	}
	rt.R().Stop()
	return nil
}

func (i *nodeInvoker) testNodeManager(ctx context.T, workspace string, envelope *application.Envelope) error {
	path := filepath.Join(workspace, "noded.sh")
	cmd := exec.Command(path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Setup up the child process callback.
	callbackState := i.callback
	listener := callbackState.listenFor(mgmt.ChildNodeManagerConfigKey)
	defer listener.cleanup()
	cfg := config.New()
	cfg.Set(mgmt.ParentNodeManagerConfigKey, listener.name())
	handle := vexec.NewParentHandle(cmd, vexec.ConfigOpt{cfg})
	// Start the child process.
	if err := handle.Start(); err != nil {
		vlog.Errorf("Start() failed: %v", err)
		return errOperationFailed
	}
	defer func() {
		if err := handle.Clean(); err != nil {
			vlog.Errorf("Clean() failed: %v", err)
		}
	}()
	// Wait for the child process to start.
	testTimeout := 10 * time.Second
	if err := handle.WaitForReady(testTimeout); err != nil {
		vlog.Errorf("WaitForReady(%v) failed: %v", testTimeout, err)
		return errOperationFailed
	}
	childName, err := listener.waitForValue(testTimeout)
	if err != nil {
		return errOperationFailed
	}
	// Check that invoking Update() succeeds.
	childName = naming.MakeTerminal(naming.Join(childName, "nm"))
	nmClient, err := node.BindNode(childName)
	if err != nil {
		vlog.Errorf("BindNode(%v) failed: %v", childName, err)
		return errOperationFailed
	}
	linkOld, pathOld, err := i.getCurrentFileInfo()
	if err != nil {
		return errOperationFailed
	}
	// Since the resolution of mtime for files is seconds, the test sleeps
	// for a second to make sure it can check whether the current symlink is
	// updated.
	time.Sleep(time.Second)
	if err := nmClient.Revert(ctx); err != nil {
		return errOperationFailed
	}
	linkNew, pathNew, err := i.getCurrentFileInfo()
	if err != nil {
		return errOperationFailed
	}
	// Check that the new node manager updated the current symbolic link.
	if !linkOld.ModTime().Before(linkNew.ModTime()) {
		vlog.Errorf("new node manager test failed")
		return errOperationFailed
	}
	// Ensure that the current symbolic link points to the same script.
	if pathNew != pathOld {
		updateLink(pathOld, i.config.CurrentLink)
		vlog.Errorf("new node manager test failed")
		return errOperationFailed
	}
	return nil
}

func generateScript(workspace string, configSettings []string, envelope *application.Envelope) error {
	path, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		vlog.Errorf("EvalSymlinks(%v) failed: %v", os.Args[0], err)
		return errOperationFailed
	}
	output := "#!/bin/bash\n"
	output += strings.Join(iconfig.QuoteEnv(append(envelope.Env, configSettings...)), " ") + " "
	output += filepath.Join(workspace, "noded") + " "
	output += strings.Join(envelope.Args, " ")
	path = filepath.Join(workspace, "noded.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0700); err != nil {
		vlog.Errorf("WriteFile(%v) failed: %v", path, err)
		return errOperationFailed
	}
	return nil
}

func (i *nodeInvoker) updateNodeManager(ctx context.T) error {
	if len(i.config.Origin) == 0 {
		return errUpdateNoOp
	}
	envelope, err := fetchEnvelope(ctx, i.config.Origin)
	if err != nil {
		return err
	}
	if envelope.Title != application.NodeManagerTitle {
		return errIncompatibleUpdate
	}
	if i.config.Envelope != nil && reflect.DeepEqual(envelope, i.config.Envelope) {
		return errUpdateNoOp
	}
	// Create new workspace.
	workspace := filepath.Join(i.config.Root, "node-manager", generateVersionDirName())
	perm := os.FileMode(0700)
	if err := os.MkdirAll(workspace, perm); err != nil {
		vlog.Errorf("MkdirAll(%v, %v) failed: %v", workspace, perm, err)
		return errOperationFailed
	}
	deferrer := func() {
		if err := os.RemoveAll(workspace); err != nil {
			vlog.Errorf("RemoveAll(%v) failed: %v", workspace, err)
		}
	}
	defer func() {
		if deferrer != nil {
			deferrer()
		}
	}()
	// Populate the new workspace with a node manager binary.
	// TODO(caprita): match identical binaries on binary metadata
	// rather than binary object name.
	sameBinary := i.config.Envelope != nil && envelope.Binary == i.config.Envelope.Binary
	if err := generateBinary(workspace, "noded", envelope, !sameBinary); err != nil {
		return err
	}
	// Populate the new workspace with a node manager script.
	configSettings, err := i.config.Save(envelope)
	if err != nil {
		return errOperationFailed
	}
	if err := generateScript(workspace, configSettings, envelope); err != nil {
		return err
	}
	if err := i.testNodeManager(ctx, workspace, envelope); err != nil {
		return err
	}
	// If the binary has changed, update the node manager symlink.
	if err := updateLink(filepath.Join(workspace, "noded.sh"), i.config.CurrentLink); err != nil {
		return err
	}
	rt.R().Stop()
	deferrer = nil
	return nil
}

func (*nodeInvoker) Install(ipc.ServerContext, string) (string, error) {
	return "", errInvalidSuffix
}

func (*nodeInvoker) Refresh(ipc.ServerContext) error {
	// TODO(jsimsa): Implement.
	return nil
}

func (*nodeInvoker) Restart(ipc.ServerContext) error {
	// TODO(jsimsa): Implement.
	return nil
}

func (*nodeInvoker) Resume(ipc.ServerContext) error {
	return errInvalidSuffix
}

func (i *nodeInvoker) Revert(call ipc.ServerContext) error {
	if i.config.Previous == "" {
		return errUpdateNoOp
	}
	updatingState := i.updating
	if updatingState.testAndSetUpdating() {
		return errInProgress
	}
	err := i.revertNodeManager()
	if err != nil {
		updatingState.unsetUpdating()
	}
	return err
}

func (*nodeInvoker) Start(ipc.ServerContext) ([]string, error) {
	return nil, errInvalidSuffix
}

func (*nodeInvoker) Stop(ipc.ServerContext, uint32) error {
	return errInvalidSuffix
}

func (*nodeInvoker) Suspend(ipc.ServerContext) error {
	// TODO(jsimsa): Implement.
	return nil
}

func (*nodeInvoker) Uninstall(ipc.ServerContext) error {
	return errInvalidSuffix
}

func (i *nodeInvoker) Update(ipc.ServerContext) error {
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	updatingState := i.updating
	if updatingState.testAndSetUpdating() {
		return errInProgress
	}
	err := i.updateNodeManager(ctx)
	if err != nil {
		updatingState.unsetUpdating()
	}
	return err
}

func (*nodeInvoker) UpdateTo(ipc.ServerContext, string) error {
	// TODO(jsimsa): Implement.
	return nil
}
