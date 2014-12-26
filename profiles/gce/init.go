// +build linux

// Package gce provides a Profile for Google Compute Engine and should be
// used by binaries that only ever expect to be run on GCE.
package gce

import (
	"flag"
	"fmt"
	"net"

	"v.io/core/veyron2"
	"v.io/core/veyron2/config"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/rt"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/profiles/internal/gce"
	"v.io/core/veyron/profiles/internal/platform"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	_ "v.io/core/veyron/runtimes/google/rt"
)

var (
	commonFlags *flags.Flags

	// ListenSpec is an initialized instance of ipc.ListenSpec that can
	// be used with ipc.Listen.
	ListenSpec ipc.ListenSpec
)

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Listen)
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
	pstr, _ := platform.Platform()
	return pstr
}

func (p *profile) String() string {
	return "net " + p.Platform().String()
}

func (p *profile) Init(veyron2.Runtime, *config.Publisher) (veyron2.AppCycle, error) {
	if !gce.RunningOnGCE() {
		return nil, fmt.Errorf("GCE profile used on a non-GCE system")
	}

	lf := commonFlags.ListenFlags()
	ListenSpec = ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	p.ac = appcycle.New()

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
