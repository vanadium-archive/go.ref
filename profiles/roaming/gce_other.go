// +build !linux

package roaming

import (
	"net"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
)

func handleGCE(veyron2.Runtime, *config.Publisher) *net.IPAddr {
	return nil
}
