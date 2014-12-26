// +build !linux

package internal

import (
	"net"

	"v.io/core/veyron2/vlog"
)

// GCEPublicAddress returns the public IP address of the GCE instance
// it is run from, or nil if run from anywhere else. The returned address
// is the public address of a 1:1 NAT tunnel to this host.
func GCEPublicAddress(vlog.Logger) *net.IPAddr {
	return nil
}
