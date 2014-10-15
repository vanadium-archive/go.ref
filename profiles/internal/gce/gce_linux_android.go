// +build linux,android

// The GCE profile shouldn't be used with android. This file solves build issues
// due to android using go 1.2.2
package gce

import (
	"net"
)

func RunningOnGCE() bool {
	panic("The GCE profile was unexpectedly used with android.")
}

func ExternalIPAddress() (net.IP, error) {
	panic("The GCE profile was unexpectedly used with android.")
}
