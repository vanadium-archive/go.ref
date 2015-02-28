// +build android

package gce

import (
	"net"
)

func RunningOnGCE() bool {
	return false
}

func ExternalIPAddress() (net.IP, error) {
	panic("The GCE profile was unexpectedly used with android.")
}
