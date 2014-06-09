package impl

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

	vexec "veyron/lib/exec"
	ibuild "veyron/services/mgmt/build"
	"veyron/services/mgmt/profile"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/build"
	"veyron2/services/mgmt/content"
	"veyron2/services/mgmt/node"
	"veyron2/vlog"
)

var updateSuffix = regexp.MustCompile(`^apps\/.*$`)

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
	// be used as a relative veyron name to identify an application,
	// installation, or instance.
	suffix string
}

var (
	errInvalidSuffix    = errors.New("invalid suffix")
	errOperationFailed  = errors.New("operation failed")
	errUpdateInProgress = errors.New("update in progress")
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

func downloadBinary(workspace, binary string) error {
	stub, err := content.BindContent(binary)
	if err != nil {
		vlog.Errorf("BindContent(%q) failed: %v", binary, err)
		return errOperationFailed
	}
	stream, err := stub.Download(rt.R().NewContext())
	if err != nil {
		vlog.Errorf("Download() failed: %v", err)
		return errOperationFailed
	}
	path := filepath.Join(workspace, "noded")
	file, err := os.Create(path)
	if err != nil {
		vlog.Errorf("Create(%q) failed: %v", path, err)
		return errOperationFailed
	}
	defer file.Close()
	for {
		bytes, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			vlog.Errorf("Recv() failed: %v", err)
			return errOperationFailed
		}
		if _, err := file.Write(bytes); err != nil {
			vlog.Errorf("Write() failed: %v", err)
			return errOperationFailed
		}
	}
	if err := stream.Finish(); err != nil {
		vlog.Errorf("Finish() failed: %v", err)
		return errOperationFailed
	}
	mode := os.FileMode(0755)
	if err := file.Chmod(mode); err != nil {
		vlog.Errorf("Chmod(%v) failed: %v", mode, err)
		return errOperationFailed
	}
	return nil
}

func fetchEnvelope(origin string) (*application.Envelope, error) {
	stub, err := application.BindRepository(origin)
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

func generateScript(workspace string, envelope *application.Envelope) error {
	output := "#!/bin/bash\n"
	output += strings.Join(envelope.Env, " ") + " " + os.Args[0] + " " + strings.Join(envelope.Args, " ")
	path := filepath.Join(workspace, "noded.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		vlog.Errorf("WriteFile(%v) failed: %v", path, err)
		return errOperationFailed
	}
	return nil
}

func updateLink(workspace string) error {
	link := filepath.Join(os.Getenv(ROOT_ENV), "stable")
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

func (i *invoker) testNodeManager(workspace string, envelope *application.Envelope) error {
	path := filepath.Join(workspace, "noded")
	cmd := exec.Command(path, envelope.Args...)
	cmd.Env = envelope.Env
	cmd.Env = vexec.SetEnv(cmd.Env, ORIGIN_ENV, naming.JoinAddressName(i.state.name, "ar"))
	cmd.Env = vexec.SetEnv(cmd.Env, TEST_UPDATE_ENV, "1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Setup up the child process callback.
	id := fmt.Sprintf("%d", rand.Int())
	handle := vexec.NewParentHandle(cmd, vexec.CallbackNameOpt(naming.JoinAddressName(i.state.name, id)))
	callbackChan := make(chan string)
	i.registerCallback(id, callbackChan)
	defer i.unregisterCallback(id)
	// Start the child process.
	if err := handle.Start(); err != nil {
		vlog.Errorf("Start() failed: %v", err)
		return errOperationFailed
	}
	// Wait for the child process to start.
	testTimeout := 2 * time.Second
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
			if err := cmd.Process.Kill(); err != nil {
				vlog.Errorf("Kill() failed: %v", err)
			}
			return errOperationFailed
		}
		if err := nmClient.Update(rt.R().NewContext()); err != nil {
			if err := cmd.Process.Kill(); err != nil {
				vlog.Errorf("Kill() failed: %v", err)
			}
			return errOperationFailed
		}
	case <-time.After(testTimeout):
		vlog.Errorf("Waiting for callback timed out")
		if err := cmd.Process.Kill(); err != nil {
			vlog.Errorf("Kill() failed: %v", err)
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
	envelope, err := fetchEnvelope(os.Getenv(ORIGIN_ENV))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(envelope, i.state.envelope) {
		// Create new workspace.
		workspace := filepath.Join(os.Getenv(ROOT_ENV), fmt.Sprintf("%v", time.Now().Format(time.RFC3339Nano)))
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
		if os.Getenv(TEST_UPDATE_ENV) == "" {
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
		} else {
			if err := os.RemoveAll(workspace); err != nil {
				vlog.Errorf("RemoveAll(%v) failed: %v", workspace, err)
			}
		}
		rt.R().Stop()
	}
	return nil
}

func (i *invoker) Install(call ipc.ServerContext) (string, error) {
	vlog.VI(0).Infof("%v.Install()", i.suffix)
	// TODO(jsimsa): Implement.
	return "", nil
}

func (i *invoker) Start(call ipc.ServerContext) ([]string, error) {
	vlog.VI(0).Infof("%v.Start()", i.suffix)
	// TODO(jsimsa): Implement.
	return make([]string, 0), nil
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
		i.state.updating = false
		return err
	case updateSuffix.MatchString(i.suffix):
		// TODO(jsimsa): Implement.
		return nil
	default:
		return errInvalidSuffix
	}
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

func (i *invoker) Shutdown(call ipc.ServerContext, deadline uint64) error {
	vlog.VI(0).Infof("%v.Shutdown(%v)", i.suffix, deadline)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Suspend(call ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Suspend()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

// CALLBACK RECEIVER INTERFACE IMPLEMENTATION

func (i *invoker) Callback(call ipc.ServerContext, name string) error {
	vlog.VI(0).Infof("%v.Callback()", i.suffix)
	i.state.channelsMutex.Lock()
	channel, ok := i.state.channels[i.suffix]
	i.state.channelsMutex.Unlock()
	if !ok {
		return errInvalidSuffix
	}
	channel <- name
	return nil
}

// invoker holds the state of a node manager content repository invocation.
type crInvoker struct {
	suffix string
}

// NewCRInvoker is the node manager content repository invoker factory.
func NewCRInvoker(suffix string) *crInvoker {
	return &crInvoker{
		suffix: suffix,
	}
}

// CONTENT REPOSITORY INTERFACE IMPLEMENTATION

func (i *crInvoker) Delete(ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Delete()", i.suffix)
	return nil
}

func (i *crInvoker) Download(_ ipc.ServerContext, stream content.ContentServiceDownloadStream) error {
	vlog.VI(0).Infof("%v.Download()", i.suffix)
	file, err := os.Open(os.Args[0])
	if err != nil {
		vlog.Errorf("Open() failed: %v", err)
		return errOperationFailed
	}
	defer file.Close()
	bufferLength := 4096
	buffer := make([]byte, bufferLength)
	for {
		n, err := file.Read(buffer)
		switch err {
		case io.EOF:
			return nil
		case nil:
			if err := stream.Send(buffer[:n]); err != nil {
				vlog.Errorf("Send() failed: %v", err)
				return errOperationFailed
			}
		default:
			vlog.Errorf("Read() failed: %v", err)
			return errOperationFailed
		}
	}
}

func (i *crInvoker) Upload(ipc.ServerContext, content.ContentServiceUploadStream) (string, error) {
	vlog.VI(0).Infof("%v.Upload()", i.suffix)
	return "", nil
}

// arState wraps state shared by different node manager application
// repository invocations.
type arState struct {
	// envelope is the node manager application envelope.
	envelope *application.Envelope
	// name is the node manager name.
	name string
}

// invoker holds the state of a node manager application repository invocation.
type arInvoker struct {
	state  *arState
	suffix string
}

// NewARInvoker is the node manager content repository invoker factory.

func NewARInvoker(state *arState, suffix string) *arInvoker {
	return &arInvoker{
		state:  state,
		suffix: suffix,
	}
}

// APPLICATION REPOSITORY INTERFACE IMPLEMENTATION

func (i *arInvoker) Match(ipc.ServerContext, []string) (application.Envelope, error) {
	vlog.VI(0).Infof("%v.Match()", i.suffix)
	envelope := application.Envelope{}
	envelope.Args = i.state.envelope.Args
	envelope.Binary = naming.JoinAddressName(i.state.name, "cr")
	envelope.Env = i.state.envelope.Env
	return envelope, nil
}
