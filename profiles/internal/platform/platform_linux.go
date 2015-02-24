package platform

import (
	"syscall"

	"v.io/v23"
)

// Platform returns the description of the Platform this process is running on.
// A default value for v23.Platform is provided even if an error is
// returned; nil is never returned for the first return result.
func Platform() (*v23.Platform, error) {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return &v23.Platform{}, err
	}
	d := &v23.Platform{
		Vendor:  "google",
		Model:   "generic",
		System:  utsStr(uts.Sysname[:]),
		Version: utsStr(uts.Version[:]),
		Release: utsStr(uts.Release[:]),
		Machine: utsStr(uts.Machine[:]),
		Node:    utsStr(uts.Nodename[:]),
	}
	return d, nil
}
