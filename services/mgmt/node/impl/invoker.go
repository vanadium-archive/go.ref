package impl

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	vexec "veyron/runtimes/google/lib/exec"
	ibuild "veyron/services/mgmt/build"
	"veyron/services/mgmt/profile"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/build"
	"veyron2/services/mgmt/content"
	"veyron2/services/mgmt/node"
	"veyron2/vlog"
)

var updateSuffix = regexp.MustCompile(`^apps\/.*$`)

// invoker holds the state of a node manager invocation.
type invoker struct {
	// envelope is the node manager application envelope.
	envelope *application.Envelope
	// origin is a veyron name that resolves to the node manager
	// envelope.
	origin string
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative veyron name to identify an application,
	// installation, or instance.
	suffix string
}

var (
	errInvalidSuffix   = errors.New("invalid suffix")
	errOperationFailed = errors.New("operation failed")
)

// NewInvoker is the invoker factory.
func NewInvoker(envelope *application.Envelope, origin, suffix string) *invoker {
	return &invoker{
		envelope: envelope,
		origin:   origin,
		suffix:   suffix,
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

func (i *invoker) Describe(call ipc.Context) (node.Description, error) {
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

func (i *invoker) IsRunnable(call ipc.Context, binary build.BinaryDescription) (bool, error) {
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

func (i *invoker) Reset(call ipc.Context, deadline uint64) error {
	vlog.VI(0).Infof("%v.Reset(%v)", i.suffix, deadline)
	// TODO(jsimsa): Implement.
	return nil
}

// APPLICATION INTERFACE IMPLEMENTATION

func downloadBinary(binary string) (string, error) {
	stub, err := content.BindContent(binary)
	if err != nil {
		vlog.Errorf("BindContent(%q) failed: %v", binary, err)
		return "", errOperationFailed
	}
	stream, err := stub.Download()
	if err != nil {
		vlog.Errorf("Download() failed: %v", err)
		return "", errOperationFailed
	}
	tmpDir, prefix := "", ""
	file, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%q, %q) failed: %v", tmpDir, prefix, err)
		return "", errOperationFailed
	}
	defer file.Close()
	for {
		bytes, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			vlog.Errorf("Recv() failed: %v", err)
			os.Remove(file.Name())
			return "", errOperationFailed
		}
		if _, err := file.Write(bytes); err != nil {
			vlog.Errorf("Write() failed: %v", err)
			os.Remove(file.Name())
			return "", errOperationFailed
		}
	}
	if err := stream.Finish(); err != nil {
		vlog.Errorf("Finish() failed: %v", err)
		os.Remove(file.Name())
		return "", errOperationFailed
	}
	mode := os.FileMode(0755)
	if err := file.Chmod(mode); err != nil {
		vlog.Errorf("Chmod(%v) failed: %v", mode, err)
		os.Remove(file.Name())
		return "", errOperationFailed
	}
	return file.Name(), nil
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
	envelope, err := stub.Match(profiles)
	if err != nil {
		vlog.Errorf("Match(%v) failed: %v", profiles, err)
		return nil, errOperationFailed
	}
	return &envelope, nil
}

func replaceBinary(oldBinary, newBinary string) error {
	// Replace the old binary with the new one.
	if err := syscall.Unlink(oldBinary); err != nil {
		vlog.Errorf("Unlink(%v) failed: %v", oldBinary, err)
		return errOperationFailed
	}
	if err := os.Rename(newBinary, oldBinary); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", newBinary, oldBinary, err)
		return errOperationFailed
	}
	return nil
}

func spawnNodeManager(envelope *application.Envelope) error {
	cmd := exec.Command(os.Args[0], envelope.Args...)
	cmd.Env = envelope.Env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	handle := vexec.NewParentHandle(cmd, "")
	if err := handle.Start(); err != nil {
		vlog.Errorf("Start() failed: %v", err)
		return errOperationFailed
	}
	if err := handle.WaitForReady(10 * time.Second); err != nil {
		vlog.Errorf("WaitForReady() failed: %v", err)
		cmd.Process.Kill()
		return errOperationFailed
	}
	rt.R().Stop()
	return nil
}

func (i *invoker) Install(call ipc.Context) (string, error) {
	vlog.VI(0).Infof("%v.Install()", i.suffix)
	// TODO(jsimsa): Implement.
	return "", nil
}

func (i *invoker) Start(call ipc.Context) ([]string, error) {
	vlog.VI(0).Infof("%v.Start()", i.suffix)
	// TODO(jsimsa): Implement.
	return make([]string, 0), nil
}

func (i *invoker) Uninstall(call ipc.Context) error {
	vlog.VI(0).Infof("%v.Uninstall()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Update(call ipc.Context) error {
	vlog.VI(0).Infof("%v.Update()", i.suffix)
	switch {
	case i.suffix == "nm":
		// This branch attempts to update the node manager updates itself.
		envelope, err := fetchEnvelope(i.origin)
		if err != nil {
			return err
		}
		if envelope.Binary != i.envelope.Binary {
			file, err := downloadBinary(envelope.Binary)
			if err != nil {
				return err
			}
			if err := replaceBinary(os.Args[0], file); err != nil {
				os.Remove(file)
				return err
			}
		}
		if !reflect.DeepEqual(envelope, i.envelope) {
			i.envelope = envelope
			if err := spawnNodeManager(i.envelope); err != nil {
				return err
			}
			// TODO(jsimsa): When Bogdan implements the shutdown API, use it
			// to stop itself (or have the caller do that).
		}
		return nil
	case updateSuffix.MatchString(i.suffix):
		// TODO(jsimsa): Implement.
		return nil
	default:
		return errInvalidSuffix
	}
}

func (i *invoker) Refresh(call ipc.Context) error {
	vlog.VI(0).Infof("%v.Refresh()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Restart(call ipc.Context) error {
	vlog.VI(0).Infof("%v.Restart()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Resume(call ipc.Context) error {
	vlog.VI(0).Infof("%v.Resume()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Shutdown(call ipc.Context, deadline uint64) error {
	vlog.VI(0).Infof("%v.Shutdown(%v)", i.suffix, deadline)
	// TODO(jsimsa): Implement.
	return nil
}

func (i *invoker) Suspend(call ipc.Context) error {
	vlog.VI(0).Infof("%v.Suspend()", i.suffix)
	// TODO(jsimsa): Implement.
	return nil
}
