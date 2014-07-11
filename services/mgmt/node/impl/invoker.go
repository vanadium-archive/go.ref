package impl

// The implementation of the node manager expects that the node
// manager installations are all organized in the following directory
// structure:
//
// VEYRON_NM_ROOT/
//   current # symlink to one of the workspaces
//   workspace-1/
//     noded - the node manager binary
//     noded.sh - a shell script to start the binary
//  ...
//   workspace-n/
//     noded - the node manager binary
//     noded.sh - a shell script to start the binary
//
// The node manager is always expected to be started through
// VEYRON_NM_ROOT/current/noded.sh, which is monitored by an init
// daemon. This provides for simple and robust updates.
//
// To update the node manager to a newer version, a new workspace is
// created and the "current" symlink is updated
// accordingly. Similarly, to revert the node manager to a previous
// version, all that is required is to update the symlink to point to
// the previous workspace.

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"veyron/lib/config"
	ibuild "veyron/services/mgmt/build"
	"veyron/services/mgmt/lib/binary"
	vexec "veyron/services/mgmt/lib/exec"
	"veyron/services/mgmt/profile"

	"veyron2/ipc"
	"veyron2/mgmt"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/build"
	"veyron2/services/mgmt/node"
	"veyron2/services/mgmt/repository"
	"veyron2/verror"
	"veyron2/vlog"
)

const CurrentWorkspace = "current"

// state wraps state shared between different node manager
// invocations.
type state struct {
	// channels maps callback identifiers to channels that are used to
	// communicate information from child processes.
	channels map[string]chan string
	// channelsMutex is a lock for coordinating concurrent access to
	// <channels>.
	channelsMutex *sync.Mutex
	// envelope is the node manager application envelope.
	envelope *application.Envelope
	// name is the node manager name.
	name string
	// previous holds the local path to the previous version of the node
	// manager.
	previous string
	// updating is a flag that records whether this instance of node
	// manager is being updated.
	updating bool
	// updatingMutex is a lock for coordinating concurrent access to
	// <updating>.
	updatingMutex *sync.Mutex
}

// invoker holds the state of a node manager invocation.
type invoker struct {
	state *state
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify an application,
	// installation, or instance.
	suffix string
}

var (
	appsSuffix = regexp.MustCompile(`^apps\/.*$`)

	errInvalidSuffix      = verror.BadArgf("invalid suffix")
	errOperationFailed    = verror.Internalf("operation failed")
	errUpdateInProgress   = verror.Existsf("update in progress")
	errIncompatibleUpdate = verror.BadArgf("update failed: mismatching app title")
)

// NewInvoker is the invoker factory.
func NewInvoker(state *state, suffix string) *invoker {
	return &invoker{
		state:  state,
		suffix: suffix,
	}
}

// NODE INTERFACE IMPLEMENTATION

// computeNodeProfile generates a description of the runtime
// environment (supported file format, OS, architecture, libraries) of
// the host node.
//
// TODO(jsimsa): Avoid computing the host node description from
// scratch if a recent cached copy exists.
func (i *invoker) computeNodeProfile() (*profile.Specification, error) {
	result := profile.Specification{Format: profile.Format{Attributes: make(map[string]string)}}

	// Find out what the supported file format, operating system, and
	// architecture is.
	switch runtime.GOOS {
	case "linux":
		result.Format.Name = ibuild.ELF.String()
		result.Format.Attributes["os"] = ibuild.LINUX.String()
	case "darwin":
		result.Format.Name = ibuild.MACH.String()
		result.Format.Attributes["os"] = ibuild.DARWIN.String()
	case "windows":
		result.Format.Name = ibuild.PE.String()
		result.Format.Attributes["os"] = ibuild.WINDOWS.String()
	default:
		return nil, errors.New("Unsupported operating system: " + runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		result.Format.Attributes["arch"] = ibuild.AMD64.String()
	case "arm":
		result.Format.Attributes["arch"] = ibuild.AMD64.String()
	case "x86":
		result.Format.Attributes["arch"] = ibuild.AMD64.String()
	default:
		return nil, errors.New("Unsupported hardware architecture: " + runtime.GOARCH)
	}

	// Find out what the installed dynamically linked libraries are.
	switch runtime.GOOS {
	case "linux":
		// For Linux, we identify what dynamically linked libraries are
		// install by parsing the output of "ldconfig -p".
		command := exec.Command("ldconfig", "-p")
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, err
		}
		buf := bytes.NewBuffer(output)
		// Throw away the first line of output from ldconfig.
		if _, err := buf.ReadString('\n'); err != nil {
			return nil, errors.New("Could not identify libraries.")
		}
		// Extract the library name and version from every subsequent line.
		result.Libraries = make(map[profile.Library]struct{})
		line, err := buf.ReadString('\n')
		for err == nil {
			words := strings.Split(strings.Trim(line, " \t\n"), " ")
			if len(words) > 0 {
				tokens := strings.Split(words[0], ".so")
				if len(tokens) != 2 {
					return nil, errors.New("Could not identify library: " + words[0])
				}
				name := strings.TrimPrefix(tokens[0], "lib")
				major, minor := "", ""
				tokens = strings.SplitN(tokens[1], ".", 3)
				if len(tokens) >= 2 {
					major = tokens[1]
				}
				if len(tokens) >= 3 {
					minor = tokens[2]
				}
				result.Libraries[profile.Library{Name: name, MajorVersion: major, MinorVersion: minor}] = struct{}{}
			}
			line, err = buf.ReadString('\n')
		}
	case "darwin":
		// TODO(jsimsa): Implement.
	case "windows":
		// TODO(jsimsa): Implement.
	default:
		return nil, errors.New("Unsupported operating system: " + runtime.GOOS)
	}
	return &result, nil
}

// getProfile gets a profile description for the given profile.
//
// TODO(jsimsa): Avoid retrieving the list of known profiles from a
// remote server if a recent cached copy exists.
func (i *invoker) getProfile(name string) (*profile.Specification, error) {
	// TODO(jsimsa): This function assumes the existence of a profile
	// server from which the profiles can be retrieved. The profile
	// server is a work in progress. When it exists, the commented out
	// code below should work.
	var profile profile.Specification
	/*
			client, err := r.NewClient()
			if err != nil {
				vlog.Errorf("NewClient() failed: %v", err)
				return nil, err
			}
			defer client.Close()
		  server := // TODO
			method := "Specification"
			inputs := make([]interface{}, 0)
			call, err := client.StartCall(server + "/" + name, method, inputs)
			if err != nil {
				vlog.Errorf("StartCall(%s, %q, %v) failed: %v\n", server + "/" + name, method, inputs, err)
				return nil, err
			}
			if err := call.Finish(&profiles); err != nil {
				vlog.Errorf("Finish(%v) failed: %v\n", &profiles, err)
				return nil, err
			}
	*/
	return &profile, nil
}

// getKnownProfiles gets a list of description for all publicly known
// profiles.
//
// TODO(jsimsa): Avoid retrieving the list of known profiles from a
// remote server if a recent cached copy exists.
func (i *invoker) getKnownProfiles() ([]profile.Specification, error) {
	// TODO(jsimsa): This function assumes the existence of a profile
	// server from which a list of known profiles can be retrieved. The
	// profile server is a work in progress. When it exists, the
	// commented out code below should work.
	knownProfiles := make([]profile.Specification, 0)
	/*
			client, err := r.NewClient()
			if err != nil {
				vlog.Errorf("NewClient() failed: %v\n", err)
				return nil, err
			}
			defer client.Close()
		  server := // TODO
			method := "List"
			inputs := make([]interface{}, 0)
			call, err := client.StartCall(server, method, inputs)
			if err != nil {
				vlog.Errorf("StartCall(%s, %q, %v) failed: %v\n", server, method, inputs, err)
				return nil, err
			}
			if err := call.Finish(&knownProfiles); err != nil {
				vlog.Errorf("Finish(&knownProfile) failed: %v\n", err)
				return nil, err
			}
	*/
	return knownProfiles, nil
}

// matchProfiles inputs a profile that describes the host node and a
// set of publicly known profiles and outputs a node description that
// identifies the publicly known profiles supported by the host node.
func (i *invoker) matchProfiles(p *profile.Specification, known []profile.Specification) node.Description {
	result := node.Description{Profiles: make(map[string]struct{})}
loop:
	for _, profile := range known {
		if profile.Format.Name != p.Format.Name {
			continue
		}
		if profile.Format.Attributes["os"] != p.Format.Attributes["os"] {
			continue
		}
		if profile.Format.Attributes["arch"] != p.Format.Attributes["arch"] {
			continue
		}
		for library := range profile.Libraries {
			// Current implementation requires exact library name and version match.
			if _, found := p.Libraries[library]; !found {
				continue loop
			}
		}
		result.Profiles[profile.Label] = struct{}{}
	}
	return result
}

func (i *invoker) Describe(call ipc.ServerContext) (node.Description, error) {
	vlog.VI(0).Infof("%v.Describe()", i.suffix)
	empty := node.Description{}
	nodeProfile, err := i.computeNodeProfile()
	if err != nil {
		return empty, err
	}
	knownProfiles, err := i.getKnownProfiles()
	if err != nil {
		return empty, err
	}
	result := i.matchProfiles(nodeProfile, knownProfiles)
	return result, nil
}

func (i *invoker) IsRunnable(call ipc.ServerContext, binary build.BinaryDescription) (bool, error) {
	vlog.VI(0).Infof("%v.IsRunnable(%v)", i.suffix, binary)
	nodeProfile, err := i.computeNodeProfile()
	if err != nil {
		return false, err
	}
	binaryProfiles := make([]profile.Specification, 0)
	for name, _ := range binary.Profiles {
		profile, err := i.getProfile(name)
		if err != nil {
			return false, err
		}
		binaryProfiles = append(binaryProfiles, *profile)
	}
	result := i.matchProfiles(nodeProfile, binaryProfiles)
	return len(result.Profiles) > 0, nil
}

func (i *invoker) Reset(call ipc.ServerContext, deadline uint64) error {
	vlog.VI(0).Infof("%v.Reset(%v)", i.suffix, deadline)
	// TODO(jsimsa): Implement.
	return nil
}

// APPLICATION INTERFACE IMPLEMENTATION

func downloadBinary(workspace, name string) error {
	data, err := binary.Download(name)
	if err != nil {
		vlog.Errorf("Download(%v) failed: %v", name, err)
		return errOperationFailed
	}
	path, perm := filepath.Join(workspace, "noded"), os.FileMode(755)
	if err := ioutil.WriteFile(path, data, perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v) failed: %v", path, perm, err)
		return errOperationFailed
	}
	return nil
}

func fetchEnvelope(origin string) (*application.Envelope, error) {
	stub, err := repository.BindApplication(origin)
	if err != nil {
		vlog.Errorf("BindRepository(%v) failed: %v", origin, err)
		return nil, errOperationFailed
	}
	// TODO(jsimsa): Include logic that computes the set of supported
	// profiles.
	profiles := []string{"test"}
	envelope, err := stub.Match(rt.R().NewContext(), profiles)
	if err != nil {
		vlog.Errorf("Match(%v) failed: %v", profiles, err)
		return nil, errOperationFailed
	}
	return &envelope, nil
}

func generateBinary(workspace string, envelope *application.Envelope, newBinary bool) error {
	if newBinary {
		// Download the new binary.
		return downloadBinary(workspace, envelope.Binary)
	}
	// Link the current binary.
	path := filepath.Join(workspace, "noded")
	if err := os.Link(os.Args[0], path); err != nil {
		vlog.Errorf("Link(%v, %v) failed: %v", os.Args[0], path, err)
		return errOperationFailed
	}
	return nil
}

// TODO(jsimsa): Replace <PreviousEnv> with a command-line flag when
// command-line flags in tests are supported.
func generateScript(workspace string, envelope *application.Envelope) error {
	path, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		vlog.Errorf("EvalSymlinks(%v) failed: %v", os.Args[0], err)
		return errOperationFailed
	}
	output := "#!/bin/bash\n"
	output += PreviousEnv + "=" + filepath.Dir(path) + " "
	output += strings.Join(envelope.Env, " ") + " "
	output += filepath.Join(workspace, "noded") + " "
	output += strings.Join(envelope.Args, " ")
	path = filepath.Join(workspace, "noded.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		vlog.Errorf("WriteFile(%v) failed: %v", path, err)
		return errOperationFailed
	}
	return nil
}

// getCurrentFileInfo returns the os.FileInfo for both the symbolic
// link $VEYRON_NM_ROOT/current and the workspace this link points to.
func getCurrentFileInfo() (os.FileInfo, os.FileInfo, error) {
	path := filepath.Join(os.Getenv(RootEnv), CurrentWorkspace)
	link, err := os.Lstat(path)
	if err != nil {
		vlog.Errorf("Lstat(%v) failed: %v", path, err)
		return nil, nil, err
	}
	workspace, err := os.Stat(path)
	if err != nil {
		vlog.Errorf("Stat(%v) failed: %v", path, err)
		return nil, nil, err
	}
	return link, workspace, nil
}

func updateLink(workspace string) error {
	link := filepath.Join(os.Getenv(RootEnv), CurrentWorkspace)
	newLink := link + ".new"
	fi, err := os.Lstat(newLink)
	if err == nil {
		if err := os.Remove(fi.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", fi.Name(), err)
			return errOperationFailed
		}
	}
	if err := os.Symlink(workspace, newLink); err != nil {
		vlog.Errorf("Symlink(%v, %v) failed: %v", workspace, newLink, err)
		return errOperationFailed
	}
	if err := os.Rename(newLink, link); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", newLink, link, err)
		return errOperationFailed
	}
	return nil
}

func (i *invoker) registerCallback(id string, channel chan string) {
	i.state.channelsMutex.Lock()
	defer i.state.channelsMutex.Unlock()
	i.state.channels[id] = channel
}

func (i *invoker) revertNodeManager() error {
	if err := updateLink(i.state.previous); err != nil {
		return err
	}
	rt.R().Stop()
	return nil
}

func (i *invoker) testNodeManager(workspace string, envelope *application.Envelope) error {
	path := filepath.Join(workspace, "noded.sh")
	cmd := exec.Command(path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Setup up the child process callback.
	id := fmt.Sprintf("%d", rand.Int())
	cfg := config.New()
	cfg.Set(mgmt.ParentNodeManagerConfigKey, naming.JoinAddressName(i.state.name, id))
	handle := vexec.NewParentHandle(cmd, vexec.ConfigOpt{cfg})
	callbackChan := make(chan string)
	i.registerCallback(id, callbackChan)
	defer i.unregisterCallback(id)
	// Start the child process.
	if err := handle.Start(); err != nil {
		vlog.Errorf("Start() failed: %v", err)
		return errOperationFailed
	}
	// Wait for the child process to start.
	testTimeout := 10 * time.Second
	if err := handle.WaitForReady(testTimeout); err != nil {
		vlog.Errorf("WaitForReady(%v) failed: %v", testTimeout, err)
		if err := cmd.Process.Kill(); err != nil {
			vlog.Errorf("Kill() failed: %v", err)
		}
		return errOperationFailed
	}
	// Wait for the child process to invoke the Callback().
	select {
	case address := <-callbackChan:
		// Check that invoking Update() succeeds.
		address = naming.JoinAddressName(address, "nm")
		nmClient, err := node.BindNode(address)
		if err != nil {
			vlog.Errorf("BindNode(%v) failed: %v", address, err)
			if err := handle.Clean(); err != nil {
				vlog.Errorf("Clean() failed: %v", err)
			}
			return errOperationFailed
		}
		linkOld, workspaceOld, err := getCurrentFileInfo()
		if err != nil {
			if err := handle.Clean(); err != nil {
				vlog.Errorf("Clean() failed: %v", err)
			}
			return errOperationFailed
		}
		// Since the resolution of mtime for files is seconds,
		// the test sleeps for a second to make sure it can
		// check whether the "current" symlink is updated.
		time.Sleep(time.Second)
		if err := nmClient.Revert(rt.R().NewContext()); err != nil {
			if err := handle.Clean(); err != nil {
				vlog.Errorf("Clean() failed: %v", err)
			}
			return errOperationFailed
		}
		linkNew, workspaceNew, err := getCurrentFileInfo()
		if err != nil {
			if err := handle.Clean(); err != nil {
				vlog.Errorf("Clean() failed: %v", err)
			}
			return errOperationFailed
		}
		// Check that the new node manager updated the symbolic link
		// $VEYRON_NM_ROOT/current.
		if !linkOld.ModTime().Before(linkNew.ModTime()) {
			vlog.Errorf("new node manager test failed")
			return errOperationFailed
		}
		// Check that the symbolic link $VEYRON_NM_ROOT/current points to
		// the same workspace.
		if workspaceOld.Name() != workspaceNew.Name() {
			updateLink(workspaceOld.Name())
			vlog.Errorf("new node manager test failed")
			return errOperationFailed
		}
	case <-time.After(testTimeout):
		vlog.Errorf("Waiting for callback timed out")
		if err := handle.Clean(); err != nil {
			vlog.Errorf("Clean() failed: %v", err)
		}
		return errOperationFailed
	}
	return nil
}

func (i *invoker) unregisterCallback(id string) {
	i.state.channelsMutex.Lock()
	defer i.state.channelsMutex.Unlock()
	delete(i.state.channels, id)
}

func (i *invoker) updateNodeManager() error {
	envelope, err := fetchEnvelope(os.Getenv(OriginEnv))
	if err != nil {
		return err
	}
	if envelope.Title != application.NodeManagerTitle {
		return errIncompatibleUpdate
	}
	if !reflect.DeepEqual(envelope, i.state.envelope) {
		// Create new workspace.
		workspace := filepath.Join(os.Getenv(RootEnv), fmt.Sprintf("%v", time.Now().Format(time.RFC3339Nano)))
		perm := os.FileMode(0755)
		if err := os.MkdirAll(workspace, perm); err != nil {
			vlog.Errorf("MkdirAll(%v, %v) failed: %v", workspace, perm, err)
			return errOperationFailed
		}
		// Populate the new workspace with a node manager binary.
		if err := generateBinary(workspace, envelope, envelope.Binary != i.state.envelope.Binary); err != nil {
			if err := os.RemoveAll(workspace); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", workspace, err)
			}
			return err
		}
		// Populate the new workspace with a node manager script.
		if err := generateScript(workspace, envelope); err != nil {
			if err := os.RemoveAll(workspace); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", workspace, err)
			}
			return err
		}
		if err := i.testNodeManager(workspace, envelope); err != nil {
			if err := os.RemoveAll(workspace); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", workspace, err)
			}
			return err
		}
		// If the binary has changed, update the node manager symlink.
		if err := updateLink(workspace); err != nil {
			if err := os.RemoveAll(workspace); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", workspace, err)
			}
			return err
		}
		rt.R().Stop()
	}
	return nil
}

func (i *invoker) Install(call ipc.ServerContext, von string) (string, error) {
	vlog.VI(0).Infof("%v.Install(%q)", i.suffix, von)
	// TODO(jsimsa): Implement.
	return "", nil
}

func (i *invoker) Refresh(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Refresh()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Restart(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Restart()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Resume(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Resume()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Revert(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Revert()", i.suffix)
	if i.state.previous == "" {
		return errOperationFailed
	}
	i.state.updatingMutex.Lock()
	if i.state.updating {
		i.state.updatingMutex.Unlock()
		return errUpdateInProgress
	} else {
		i.state.updating = true
	}
	i.state.updatingMutex.Unlock()
	err := i.revertNodeManager()
	i.state.updatingMutex.Lock()
	i.state.updating = false
	i.state.updatingMutex.Unlock()
	return err
}

func (i *invoker) Start(call ipc.ServerContext) ([]string, error) {
	vlog.VI(0).Infof("%v.Start()", i.suffix)
	// TODO(jsimsa): Implement.
	return make([]string, 0), nil
}

func (i *invoker) Stop(call ipc.ServerContext, deadline uint64) error {
	vlog.VI(0).Infof("%v.Stop(%v)", i.suffix, deadline)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Suspend(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Suspend()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Uninstall(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Uninstall()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Update(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Update()", i.suffix)
	switch {
	case i.suffix == "nm":
		// This branch attempts to update the node manager itself.
		i.state.updatingMutex.Lock()
		if i.state.updating {
			i.state.updatingMutex.Unlock()
			return errUpdateInProgress
		} else {
			i.state.updating = true
		}
		i.state.updatingMutex.Unlock()
		err := i.updateNodeManager()
		i.state.updatingMutex.Lock()
		i.state.updating = false
		i.state.updatingMutex.Unlock()
		return err
	case appsSuffix.MatchString(i.suffix):
		// TODO(jsimsa): Implement.
		return nil
	default:
		return errInvalidSuffix
	}

}

func (i *invoker) UpdateTo(call ipc.ServerContext, von string) error {
	vlog.VI(0).Infof("%v.UpdateTo(%q)", i.suffix, von)
	// TODO(jsimsa): Implement.
	return nil
}

// CONFIG INTERFACE IMPLEMENTATION

func (i *invoker) Set(_ ipc.ServerContext, key, value string) error {
	vlog.VI(0).Infof("%v.Set(%v, %v)", i.suffix, key, value)
	// For now, only handle the child node manager name.  We'll add handling
	// for the child's app cycle manager name later on.
	if key != mgmt.ChildNodeManagerConfigKey {
		return nil
	}
	i.state.channelsMutex.Lock()
	channel, ok := i.state.channels[i.suffix]
	i.state.channelsMutex.Unlock()
	if !ok {
		return errInvalidSuffix
	}
	channel <- value
	return nil
}
