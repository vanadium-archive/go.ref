// +build linux,veyronbluetooth,!android

package bluetooth

import (
	"veyron/lib/bluetooth"

	"veyron2/ipc/stream"
)

func registerBT() {
	stream.RegisterProtocol(bluetooth.Network, bluetooth.Dial, bluetooth.Listen)
}

func (p *profile) String() string {
	return "net/bluetooth " + p.Platform().String()
}
