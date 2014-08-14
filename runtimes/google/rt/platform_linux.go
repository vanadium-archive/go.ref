// +build linux

package rt

import (
	"fmt"
	"syscall"

	"veyron2"
	"veyron2/security"
)

func str(c []int8) string {
	r := ""
	for i, v := range c {
		if i == 0 {
			// strip terminating null
			return c[:i]
		}
	}
	// no terminating null
	return string(c)
}

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
		System:  str(uts.Sysname[:]),
		Version: str(uts.Version[:]),
		Release: str(uts.Release[:]),
		Machine: str(uts.Machine[:]),
		Node:    str(uts.Nodename[:]),
	}
	d.Identity = security.FakePublicID(fmt.Sprintf("%s/%s/%s", d.Vendor, d.Model, d.Node))
	return d, nil
}
