// +build linux

package net

import (
	"net"

	"veyron2"
	"veyron2/config"

	"veyron/profiles/internal/gce"
)

func handleGCE(rt veyron2.Runtime, publisher *config.Publisher) bool {
	log := rt.Logger()
	if gce.RunningOnGCE() {
		var pub net.IP
		// Determine the IP address from GCE's metadata
		if ip, err := gce.ExternalIPAddress(); err != nil {
			log.Infof("failed to query GCE metadata: %s", err)
		} else {
			// 1:1 NAT case, our network config will not change.
			pub = ip
		}
		if pub == nil {
			log.Infof("failed to determine public IP address to publish with")
		}

		ch := make(chan config.Setting)
		defer close(ch)
		if _, err := publisher.CreateStream(StreamName, "network configuration", ch); err != nil {
			return false
		}
		publishInitialSettings(ch, listen_protocol, listen_addr.IP.String(), pub)
		return true
	}
	return false
}
