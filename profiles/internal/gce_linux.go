// +build linux

package internal

import (
	"net"

	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/profiles/internal/gce"
)

// GCEPublicAddress returns the public IP address of the GCE instance
// it is run from, or nil if run from anywhere else. The returned address
// is the public address of a 1:1 NAT tunnel to this host.
func GCEPublicAddress(log vlog.Logger) *net.IPAddr {
	if !gce.RunningOnGCE() {
		return nil
	}
	var pub *net.IPAddr
	// Determine the IP address from GCE's metadata
	if ip, err := gce.ExternalIPAddress(); err != nil {
		log.Infof("failed to query GCE metadata: %s", err)
	} else {
		// 1:1 NAT case, our network config will not change.
		pub = &net.IPAddr{IP: ip}
	}
	if pub == nil {
		log.Infof("failed to determine public IP address to publish with")
	}
	return pub
}
