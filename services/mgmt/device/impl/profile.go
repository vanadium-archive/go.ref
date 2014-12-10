package impl

import (
	"bytes"
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"veyron.io/veyron/veyron/services/mgmt/profile"

	"veyron.io/veyron/veyron2/services/mgmt/build"
	"veyron.io/veyron/veyron2/services/mgmt/device"
)

// computeDeviceProfile generates a description of the runtime
// environment (supported file format, OS, architecture, libraries) of
// the host device.
//
// TODO(jsimsa): Avoid computing the host device description from
// scratch if a recent cached copy exists.
func computeDeviceProfile() (*profile.Specification, error) {
	result := profile.Specification{}

	// Find out what the supported file format, operating system, and
	// architecture is.
	switch runtime.GOOS {
	case "darwin":
		result.Format = build.MACH
		result.OS = build.Darwin
	case "linux":
		result.Format = build.ELF
		result.OS = build.Linux
	case "windows":
		result.Format = build.PE
		result.OS = build.Windows
	default:
		return nil, errors.New("Unsupported operating system: " + runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		result.Arch = build.AMD64
	case "arm":
		result.Arch = build.ARM
	case "x86":
		result.Arch = build.X86
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
func getProfile(name string) (*profile.Specification, error) {
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
func getKnownProfiles() ([]profile.Specification, error) {
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

// matchProfiles inputs a profile that describes the host device and a
// set of publicly known profiles and outputs a device description that
// identifies the publicly known profiles supported by the host device.
func matchProfiles(p *profile.Specification, known []profile.Specification) device.Description {
	result := device.Description{Profiles: make(map[string]struct{})}
loop:
	for _, profile := range known {
		if profile.Format != p.Format {
			continue
		}
		if profile.OS != p.OS {
			continue
		}
		if profile.Arch != p.Arch {
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
