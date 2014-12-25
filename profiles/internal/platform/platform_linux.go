package platform

import (
	"syscall"

	"v.io/veyron/veyron2"
)

// Platform returns the description of the Platform this process is running on.
// A default value for veyron2.Platform is provided even if an error is
// returned; nil is never returned for the first return result.
func Platform() (*veyron2.Platform, error) {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return &veyron2.Platform{}, err
	}
	d := &veyron2.Platform{
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
