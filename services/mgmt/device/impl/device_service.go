package impl

// The device invoker is responsible for managing the state of the device
// manager itself.  The implementation expects that the device manager
// installations are all organized in the following directory structure:
//
// <config.Root>/
//   device-manager/
//     info                    - metadata for the device manager (such as object
//                               name and process id)
//     logs/                   - device manager logs
//       STDERR-<timestamp>    - one for each execution of device manager
//       STDOUT-<timestamp>    - one for each execution of device manager
//     <version 1 timestamp>/  - timestamp of when the version was downloaded
//       deviced               - the device manager binary
//       deviced.sh            - a shell script to start the binary
//     <version 2 timestamp>
//     ...
//     device-data/
//       acls/
//         data
//         signature
//	 associated.accounts
//       persistent-args       - list of persistent arguments for the device
//                               manager (json encoded)
//
// The device manager is always expected to be started through the symbolic link
// passed in as config.CurrentLink, which is monitored by an init daemon. This
// provides for simple and robust updates.
//
// To update the device manager to a newer version, a new workspace is created
// and the symlink is updated to the new deviced.sh script. Similarly, to revert
// the device manager to a previous version, all that is required is to update
// the symlink to point to the previous deviced.sh script.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/mgmt"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/mgmt/binary"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/flags/buildinfo"

	vexec "v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/flags/consts"
	vsecurity "v.io/x/ref/security"
	"v.io/x/ref/services/mgmt/device/config"
	"v.io/x/ref/services/mgmt/profile"
)

type updatingState struct {
	// updating is a flag that records whether this instance of device
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

// deviceService implements the Device manager's Device interface.
type deviceService struct {
	updating       *updatingState
	restartHandler func()
	callback       *callbackState
	config         *config.State
	disp           *dispatcher
	uat            BlessingSystemAssociationStore
	securityAgent  *securityAgentState
}

// Version info for this device manager binary. Increment as appropriate when the binary changes.
// The major version number should be incremented whenever a change to the binary makes it incompatible
// with on-disk state created by a binary from a different major version.
type Version struct{ Major, Minor int }

var CurrentVersion = Version{1, 0}

// CreationInfo holds data about the binary that originally created the device manager on-disk state
type CreatorInfo struct {
	Version   Version
	BuildInfo string
}

func SaveCreatorInfo(dir string) error {
	info := CreatorInfo{
		Version:   CurrentVersion,
		BuildInfo: buildinfo.Info().String(),
	}
	jsonInfo, err := json.Marshal(info)
	if err != nil {
		vlog.Errorf("Marshal(%v) failed: %v", info, err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err := os.MkdirAll(dir, os.FileMode(0700)); err != nil {
		vlog.Errorf("MkdirAll(%v) failed: %v", dir, err)
		return verror.New(ErrOperationFailed, nil)
	}
	infoPath := filepath.Join(dir, "creation_info")
	if err := ioutil.WriteFile(infoPath, jsonInfo, 0600); err != nil {
		vlog.Errorf("WriteFile(%v) failed: %v", infoPath, err)
		return verror.New(ErrOperationFailed, nil)
	}
	// Make the file read-only as we don't want anyone changing it
	if err := os.Chmod(infoPath, 0400); err != nil {
		vlog.Errorf("Chmod(0400, %v) failed: %v", infoPath, err)
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
}

func loadCreatorInfo(dir string) (*CreatorInfo, error) {
	infoPath := filepath.Join(dir, "creation_info")
	info := new(CreatorInfo)
	if infoBytes, err := ioutil.ReadFile(infoPath); err != nil {
		vlog.Errorf("ReadFile(%v) failed: %v", infoPath, err)
		return nil, verror.New(ErrOperationFailed, nil)
	} else if err := json.Unmarshal(infoBytes, info); err != nil {
		vlog.Errorf("Unmarshal(%v) failed: %v", infoBytes, err)
		return nil, verror.New(ErrOperationFailed, nil)
	}
	return info, nil
}

// Checks the compatibilty of the running binary against the device manager directory on disk
func CheckCompatibility(dir string) error {
	if infoOnDisk, err := loadCreatorInfo(dir); err != nil {
		vlog.Errorf("Failed to load creator info from %s", dir)
		return verror.New(ErrOperationFailed, nil)
	} else if CurrentVersion.Major != infoOnDisk.Version.Major {
		vlog.Errorf("Device Manager binary vs disk major version mismatch (%+v vs %+v)",
			CurrentVersion, infoOnDisk.Version)
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
}

// ManagerInfo holds state about a running device manager or a running agentd
type ManagerInfo struct {
	Pid int
}

func SaveManagerInfo(dir string, info *ManagerInfo) error {
	jsonInfo, err := json.Marshal(info)
	if err != nil {
		return verror.New(ErrOperationFailed, nil, fmt.Sprintf("Marshal(%v) failed: %v", info, err))
	}
	if err := os.MkdirAll(dir, os.FileMode(0700)); err != nil {
		return verror.New(ErrOperationFailed, nil, fmt.Sprintf("MkdirAll(%v) failed: %v", dir, err))
	}
	infoPath := filepath.Join(dir, "info")
	if err := ioutil.WriteFile(infoPath, jsonInfo, 0600); err != nil {
		return verror.New(ErrOperationFailed, nil, fmt.Sprintf("WriteFile(%v) failed: %v", infoPath, err))
	}
	return nil
}

func loadManagerInfo(dir string) (*ManagerInfo, error) {
	infoPath := filepath.Join(dir, "info")
	info := new(ManagerInfo)
	if infoBytes, err := ioutil.ReadFile(infoPath); err != nil {
		return nil, verror.New(ErrOperationFailed, nil, fmt.Sprintf("ReadFile(%v) failed: %v", infoPath, err))
	} else if err := json.Unmarshal(infoBytes, info); err != nil {
		return nil, verror.New(ErrOperationFailed, nil, fmt.Sprintf("Unmarshal(%v) failed: %v", infoBytes, err))
	}
	return info, nil
}

func savePersistentArgs(root string, args []string) error {
	dir := filepath.Join(root, "device-manager", "device-data")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("MkdirAll(%q) failed: %v", dir)
	}
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("Marshal(%v) failed: %v", args, err)
	}
	fileName := filepath.Join(dir, "persistent-args")
	return ioutil.WriteFile(fileName, data, 0600)
}

func loadPersistentArgs(root string) ([]string, error) {
	fileName := filepath.Join(root, "device-manager", "device-data", "persistent-args")
	bytes, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	args := []string{}
	if err := json.Unmarshal(bytes, &args); err != nil {
		return nil, fmt.Errorf("json.Unmarshal(%v) failed: %v", bytes, err)
	}
	return args, nil
}

func (*deviceService) Describe(ipc.ServerCall) (device.Description, error) {
	return Describe()
}

func (*deviceService) IsRunnable(_ ipc.ServerCall, description binary.Description) (bool, error) {
	deviceProfile, err := ComputeDeviceProfile()
	if err != nil {
		return false, err
	}
	binaryProfiles := make([]*profile.Specification, 0)
	for name, _ := range description.Profiles {
		profile, err := getProfile(name)
		if err != nil {
			return false, err
		}
		binaryProfiles = append(binaryProfiles, profile)
	}
	result := matchProfiles(deviceProfile, binaryProfiles)
	return len(result.Profiles) > 0, nil
}

func (*deviceService) Reset(call ipc.ServerCall, deadline uint64) error {
	// TODO(jsimsa): Implement.
	return nil
}

// getCurrentFileInfo returns the os.FileInfo for both the symbolic link
// CurrentLink, and the device script in the workspace that this link points to.
func (s *deviceService) getCurrentFileInfo() (os.FileInfo, string, error) {
	path := s.config.CurrentLink
	link, err := os.Lstat(path)
	if err != nil {
		return nil, "", verror.New(ErrOperationFailed, nil, fmt.Sprintf("Lstat(%v) failed: %v", path, err))
	}
	scriptPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", verror.New(ErrOperationFailed, nil, fmt.Sprintf("EvalSymlinks(%v) failed: %v", path, err))
	}
	return link, scriptPath, nil
}

func (s *deviceService) revertDeviceManager(ctx *context.T) error {
	if err := updateLink(s.config.Previous, s.config.CurrentLink); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("updateLink failed: %v", err))
	}
	if s.restartHandler != nil {
		s.restartHandler()
	}
	v23.GetAppCycle(ctx).Stop()
	return nil
}

func (s *deviceService) newLogfile(prefix string) (*os.File, error) {
	d := filepath.Join(s.config.Root, "device_test_logs")
	if _, err := os.Stat(d); err != nil {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
	}
	f, err := ioutil.TempFile(d, "__device_impl_test__"+prefix)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// TODO(cnicolaou): would this be better implemented using the modules
// framework now that it exists?
func (s *deviceService) testDeviceManager(ctx *context.T, workspace string, envelope *application.Envelope) error {
	path := filepath.Join(workspace, "deviced.sh")
	cmd := exec.Command(path)
	cmd.Env = []string{"DEVICE_MANAGER_DONT_REDIRECT_STDOUT_STDERR=1"}

	for k, v := range map[string]*io.Writer{
		"stdout": &cmd.Stdout,
		"stderr": &cmd.Stderr,
	} {
		// Using a log file makes it less likely that stdout and stderr
		// output will be lost if the child crashes.
		file, err := s.newLogfile(fmt.Sprintf("deviced-test-%s", k))
		if err != nil {
			return err
		}
		fName := file.Name()
		defer os.Remove(fName)
		*v = file

		defer func() {
			if f, err := os.Open(fName); err == nil {
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					vlog.Infof("[testDeviceManager %s] %s", k, scanner.Text())
				}
			}
		}()
	}

	// Setup up the child process callback.
	callbackState := s.callback
	listener := callbackState.listenFor(mgmt.ChildNameConfigKey)
	defer listener.cleanup()
	cfg := vexec.NewConfig()

	cfg.Set(mgmt.ParentNameConfigKey, listener.name())
	cfg.Set(mgmt.ProtocolConfigKey, "tcp")
	cfg.Set(mgmt.AddressConfigKey, "127.0.0.1:0")

	var p security.Principal
	var agentHandle []byte
	if s.securityAgent != nil {
		// TODO(rthellend): Cleanup principal
		handle, conn, err := s.securityAgent.keyMgrAgent.NewPrincipal(ctx, false)
		if err != nil {
			return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("NewPrincipal() failed %v", err))
		}
		agentHandle = handle
		var cancel func()
		if p, cancel, err = agentPrincipal(ctx, conn); err != nil {
			return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("agentPrincipal failed: %v", err))
		}
		defer cancel()

	} else {
		credentialsDir := filepath.Join(workspace, "credentials")
		var err error
		if p, err = vsecurity.CreatePersistentPrincipal(credentialsDir, nil); err != nil {
			return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("CreatePersistentPrincipal(%v, nil) failed: %v", credentialsDir, err))
		}
		cmd.Env = append(cmd.Env, consts.VeyronCredentials+"="+credentialsDir)
	}
	dmPrincipal := v23.GetPrincipal(ctx)
	dmBlessings, err := dmPrincipal.Bless(p.PublicKey(), dmPrincipal.BlessingStore().Default(), "testdm", security.UnconstrainedUse())
	if err := p.BlessingStore().SetDefault(dmBlessings); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("BlessingStore.SetDefault() failed: %v", err))
	}
	if _, err := p.BlessingStore().Set(dmBlessings, security.AllPrincipals); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("BlessingStore.Set() failed: %v", err))
	}
	if err := p.AddToRoots(dmBlessings); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("AddToRoots() failed: %v", err))
	}

	if s.securityAgent != nil {
		file, err := s.securityAgent.keyMgrAgent.NewConnection(agentHandle)
		if err != nil {
			return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("NewConnection(%v) failed: %v", agentHandle, err))
		}
		defer file.Close()

		fd := len(cmd.ExtraFiles) + vexec.FileOffset
		cmd.ExtraFiles = append(cmd.ExtraFiles, file)
		cfg.Set(mgmt.SecurityAgentFDConfigKey, strconv.Itoa(fd))
	}

	handle := vexec.NewParentHandle(cmd, vexec.ConfigOpt{cfg})
	// Start the child process.
	if err := handle.Start(); err != nil {
		vlog.Errorf("Start() failed: %v", err)
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("Start() failed: %v", err))
	}
	defer func() {
		if err := handle.Clean(); err != nil {
			vlog.Errorf("Clean() failed: %v", err)
		}
	}()
	// Wait for the child process to start.
	if err := handle.WaitForReady(childReadyTimeout); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("WaitForReady(%v) failed: %v", childReadyTimeout, err))
	}
	childName, err := listener.waitForValue(childReadyTimeout)
	if err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("waitForValue(%v) failed: %v", childReadyTimeout, err))
	}
	// Check that invoking Stop() succeeds.
	childName = naming.Join(childName, "device")
	dmClient := device.DeviceClient(childName)
	if err := dmClient.Stop(ctx, 0); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("Stop() failed: %v", err))
	}
	if err := handle.Wait(childWaitTimeout); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("New device manager failed to exit cleanly: %v", err))
	}
	return nil
}

// TODO(caprita): Move this to util.go since device_installer is also using it now.

func generateScript(workspace string, configSettings []string, envelope *application.Envelope, logs string) error {
	// TODO(caprita): Remove this snippet of code, it doesn't seem to serve
	// any purpose.
	path, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		return verror.New(ErrOperationFailed, nil, fmt.Sprintf("EvalSymlinks(%v) failed: %v", os.Args[0], err))
	}

	if err := os.MkdirAll(logs, 0700); err != nil {
		return verror.New(ErrOperationFailed, nil, fmt.Sprintf("MkdirAll(%v) failed: %v", logs, err))
	}
	stderrLog, stdoutLog := filepath.Join(logs, "STDERR"), filepath.Join(logs, "STDOUT")

	output := "#!/bin/bash\n"
	output += "if [ -z \"$DEVICE_MANAGER_DONT_REDIRECT_STDOUT_STDERR\" ]; then\n"
	output += fmt.Sprintf("  TIMESTAMP=$(%s)\n", dateCommand)
	output += fmt.Sprintf("  exec > %s-$TIMESTAMP 2> %s-$TIMESTAMP\n", stdoutLog, stderrLog)
	output += "  LOG_TO_STDERR=false\n"
	output += "else\n"
	output += "  LOG_TO_STDERR=true\n"
	output += "fi\n"
	output += strings.Join(config.QuoteEnv(append(envelope.Env, configSettings...)), " ") + " "
	// Escape the path to the binary; %q uses Go-syntax escaping, but it's
	// close enough to Bash that we're using it as an approximation.
	//
	// TODO(caprita/rthellend): expose and use shellEscape (from
	// veyron/tools/debug/impl.go) instead.
	output += fmt.Sprintf("exec %q", filepath.Join(workspace, "deviced")) + " "
	output += fmt.Sprintf("--log_dir=%q ", logs)
	output += "--logtostderr=${LOG_TO_STDERR} "
	output += strings.Join(envelope.Args, " ")

	path = filepath.Join(workspace, "deviced.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0700); err != nil {
		return verror.New(ErrOperationFailed, nil, fmt.Sprintf("WriteFile(%v) failed: %v", path, err))
	}
	return nil
}

func (s *deviceService) updateDeviceManager(ctx *context.T) error {
	if len(s.config.Origin) == 0 {
		return verror.New(ErrUpdateNoOp, ctx)
	}
	envelope, err := fetchEnvelope(ctx, s.config.Origin)
	if err != nil {
		return err
	}
	if envelope.Title != application.DeviceManagerTitle {
		return verror.New(ErrAppTitleMismatch, ctx, fmt.Sprintf("app title mismatch. Got %q, expected %q.", envelope.Title, application.DeviceManagerTitle))
	}
	// Read and merge persistent args, if present.
	if args, err := loadPersistentArgs(s.config.Root); err == nil {
		envelope.Args = append(envelope.Args, args...)
	}
	if s.config.Envelope != nil && reflect.DeepEqual(envelope, s.config.Envelope) {
		return verror.New(ErrUpdateNoOp, ctx)
	}
	// Create new workspace.
	workspace := filepath.Join(s.config.Root, "device-manager", generateVersionDirName())
	perm := os.FileMode(0700)
	if err := os.MkdirAll(workspace, perm); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("MkdirAll(%v, %v) failed: %v", workspace, perm, err))
	}

	deferrer := func() {
		cleanupDir(workspace, "")
	}
	defer func() {
		if deferrer != nil {
			deferrer()
		}
	}()

	// Populate the new workspace with a device manager binary.
	// TODO(caprita): match identical binaries on binary signature
	// rather than binary object name.
	sameBinary := s.config.Envelope != nil && envelope.Binary.File == s.config.Envelope.Binary.File
	if sameBinary {
		if err := linkSelf(workspace, "deviced"); err != nil {
			return err
		}
	} else {
		if err := downloadBinary(ctx, envelope.Publisher, &envelope.Binary, workspace, "deviced"); err != nil {
			return err
		}
	}

	// Populate the new workspace with a device manager script.
	configSettings, err := s.config.Save(envelope)
	if err != nil {
		return verror.New(ErrOperationFailed, ctx, err)
	}

	logs := filepath.Join(s.config.Root, "device-manager", "logs")
	if err := generateScript(workspace, configSettings, envelope, logs); err != nil {
		return err
	}

	if err := s.testDeviceManager(ctx, workspace, envelope); err != nil {
		return verror.New(ErrOperationFailed, ctx, fmt.Sprintf("testDeviceManager failed: %v", err))
	}

	if err := updateLink(filepath.Join(workspace, "deviced.sh"), s.config.CurrentLink); err != nil {
		return err
	}

	if s.restartHandler != nil {
		s.restartHandler()
	}
	v23.GetAppCycle(ctx).Stop()
	deferrer = nil
	return nil
}

func (*deviceService) Install(call ipc.ServerCall, _ string, _ device.Config, _ application.Packages) (string, error) {
	return "", verror.New(ErrInvalidSuffix, call.Context())
}

func (*deviceService) Refresh(ipc.ServerCall) error {
	// TODO(jsimsa): Implement.
	return nil
}

func (*deviceService) Restart(ipc.ServerCall) error {
	// TODO(jsimsa): Implement.
	return nil
}

func (*deviceService) Resume(call ipc.ServerCall) error {
	return verror.New(ErrInvalidSuffix, call.Context())
}

func (s *deviceService) Revert(call ipc.ServerCall) error {
	if s.config.Previous == "" {
		return verror.New(ErrUpdateNoOp, call.Context(), fmt.Sprintf("Revert failed: no previous version"))
	}
	updatingState := s.updating
	if updatingState.testAndSetUpdating() {
		return verror.New(ErrOperationInProgress, call.Context(), fmt.Sprintf("Revert failed: already in progress"))
	}
	err := s.revertDeviceManager(call.Context())
	if err != nil {
		updatingState.unsetUpdating()
	}
	return err
}

func (*deviceService) Start(call ipc.ServerCall) ([]string, error) {
	return nil, verror.New(ErrInvalidSuffix, call.Context())
}

func (*deviceService) Stop(call ipc.ServerCall, _ uint32) error {
	v23.GetAppCycle(call.Context()).Stop()
	return nil
}

func (s *deviceService) Suspend(call ipc.ServerCall) error {
	// TODO(caprita): move this to Restart and disable Suspend for device
	// manager?
	if s.restartHandler != nil {
		s.restartHandler()
	}
	v23.GetAppCycle(call.Context()).Stop()
	return nil
}

func (*deviceService) Uninstall(call ipc.ServerCall) error {
	return verror.New(ErrInvalidSuffix, call.Context())
}

func (s *deviceService) Update(call ipc.ServerCall) error {
	ctx, cancel := context.WithTimeout(call.Context(), ipcContextLongTimeout)
	defer cancel()

	updatingState := s.updating
	if updatingState.testAndSetUpdating() {
		return verror.New(ErrOperationInProgress, call.Context())
	}

	err := s.updateDeviceManager(ctx)
	if err != nil {
		updatingState.unsetUpdating()
	}
	return err
}

func (*deviceService) UpdateTo(ipc.ServerCall, string) error {
	// TODO(jsimsa): Implement.
	return nil
}

func (s *deviceService) SetPermissions(_ ipc.ServerCall, acl access.Permissions, etag string) error {
	d := aclDir(s.disp.config)
	return s.disp.aclstore.Set(d, acl, etag)
}

func (s *deviceService) GetPermissions(ipc.ServerCall) (acl access.Permissions, etag string, err error) {
	d := aclDir(s.disp.config)
	return s.disp.aclstore.Get(d)
}

// TODO(rjkroege): Make it possible for users on the same system to also
// associate their accounts with their identities.
func (s *deviceService) AssociateAccount(call ipc.ServerCall, identityNames []string, accountName string) error {
	if accountName == "" {
		return s.uat.DisassociateSystemAccountForBlessings(identityNames)
	}
	// TODO(rjkroege): Optionally verify here that the required uname is a valid.
	return s.uat.AssociateSystemAccountForBlessings(identityNames, accountName)
}

func (s *deviceService) ListAssociations(call ipc.ServerCall) (associations []device.Association, err error) {
	// Temporary code. Dump this.
	if vlog.V(2) {
		b, r := call.RemoteBlessings().ForCall(call)
		vlog.Infof("ListAssociations given blessings: %v\n", b)
		if len(r) > 0 {
			vlog.Infof("ListAssociations rejected blessings: %v\n", r)
		}
	}
	return s.uat.AllBlessingSystemAssociations()
}

func (*deviceService) Debug(ipc.ServerCall) (string, error) {
	return "Not implemented", nil
}
