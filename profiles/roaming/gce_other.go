// +build !linux

package roaming

import (
	"net"

	"veyron2"
	"veyron2/config"
)

func handleGCE(veyron2.Runtime, *config.Publisher) *net.IPAddr {
	return nil
}
