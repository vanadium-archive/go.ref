// Package bluetooth provides support for bluetooth, conditionally compiled
// for linux with the veyronbluetooth tag but not on android.
package bluetooth

import (
	"veyron2"
	"veyron2/config"
	"veyron2/rt"

	"veyron/profiles"
	"veyron/profiles/net"
)

func init() {
	registerBT()
	// Arguably, the registration should be conditionally compiled.
	// Buf if it is, it becomes hard for the developer to know if bluetooth
	// was event attempted.
	// TODO(cnicolaou): use this in a couple of examples and see how it
	// works out in practice.
	rt.RegisterProfile(&profile{})
}

type profile struct{ net veyron2.Profile }

func (p *profile) Platform() *veyron2.Platform {
	platform, _ := profiles.Platform()
	return platform
}

func (p *profile) Name() string {
	return "net/bluetooth"
}

func (p *profile) Runtime() string {
	return ""
}

func (p *profile) Init(rt veyron2.Runtime, publisher *config.Publisher) {
	p.net = net.New()
	p.net.Init(rt, publisher)
}
