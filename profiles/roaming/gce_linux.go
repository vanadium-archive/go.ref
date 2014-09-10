// +build linux

package roaming

import (
	"net"

	"veyron2"
	"veyron2/config"

	"veyron/profiles/internal/gce"
)

func handleGCE(rt veyron2.Runtime, publisher *config.Publisher) *net.IPAddr {
	log := rt.Logger()
	if gce.RunningOnGCE() {
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
			return nil
		}

		ch := make(chan config.Setting)
		defer close(ch)
		stop, err := publisher.CreateStream(SettingsStreamName, "dhcp", ch)
		if err != nil {
			return nil
		}

		_ = stop
		// TODO(cnicolaou): stop should be used by the soon to be added
		// Cleanup method.
		return pub
	}
	return nil
}
