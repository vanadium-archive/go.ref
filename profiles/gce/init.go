// +build linux

// Package gce provides a Profile for Google Compute Engine and should be
// used by binaries that only ever expect to be run on GCE.
package gce

import (
	"flag"
	"fmt"
	"net"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/appcycle"
	"veyron.io/veyron/veyron/lib/flags"
	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/profiles/internal/gce"
)

var (
	listenAddressFlag = flags.IPHostPortFlag{Port: "0"}

	ListenSpec = &ipc.ListenSpec{
		Protocol: "tcp",
		Address:  "127.0.0.1:0",
	}
)

func init() {
	flag.Var(&listenAddressFlag, "veyron.tcp.address", "address to listen on")

	rt.RegisterProfile(&profile{})
}

type profile struct {
	ac *appcycle.AppCycle
}

func (p *profile) Name() string {
	return "GCE"
}

func (p *profile) Runtime() (string, []veyron2.ROpt) {
	return "", nil
}

func (p *profile) Platform() *veyron2.Platform {
	platform, _ := profiles.Platform()
	return platform
}

func (p *profile) String() string {
	return "net " + p.Platform().String()
}

func (p *profile) Init(rt veyron2.Runtime, publisher *config.Publisher) (veyron2.AppCycle, error) {
	if !gce.RunningOnGCE() {
		return nil, fmt.Errorf("GCE profile used on a non-GCE system")
	}
	p.ac = appcycle.New(rt)
	ListenSpec.Address = listenAddressFlag.String()
	if ip, err := gce.ExternalIPAddress(); err != nil {
		return p.ac, err
	} else {
		ListenSpec.AddressChooser = func(network string, addrs []ipc.Address) ([]ipc.Address, error) {
			return []ipc.Address{&netstate.AddrIfc{&net.IPAddr{IP: ip}, "gce-nat", nil}}, nil
		}
	}
	return p.ac, nil
}

func (p *profile) Cleanup() {
	p.ac.Shutdown()
}
