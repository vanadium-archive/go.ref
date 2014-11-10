// Package static provides a network-aware Profile that provides appropriate
// options and configuration for a variety of network configurations, including
// being behind 1-1 NATs, but without the ability to respond to dhcp changes.
package static

import (
	"flag"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/flags"
	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/profiles/internal"
	"veyron.io/veyron/veyron/services/mgmt/debug"
	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "veyron.io/veyron/veyron/security/flag"
)

var (
	commonFlags *flags.Flags
	// ListenSpec is an initialized instance of ipc.ListenSpec that can
	// be used with ipc.Listen.
	ListenSpec ipc.ListenSpec
)

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Listen)
	rt.RegisterProfile(New())
}

type static struct {
	gce string
}

// New returns a new instance of a very static Profile. It can be used
// as a default by Runtime implementations, in unit tests etc.
func New() veyron2.Profile {
	return &static{}
}

func (p *static) Name() string {
	return "static" + p.gce
}

func (p *static) Runtime() string {
	return "google"
}

func (*static) Platform() *veyron2.Platform {
	p, _ := profiles.Platform()
	return p
}

func (p *static) Init(rt veyron2.Runtime, _ *config.Publisher) error {
	log := rt.Logger()

	rt.ConfigureReservedName(debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie(), rt.VtraceStore()))

	lf := commonFlags.ListenFlags()
	ListenSpec = ipc.ListenSpec{
		Protocol: lf.ListenProtocol.Protocol,
		Address:  lf.ListenAddress.String(),
		Proxy:    lf.ListenProxy,
	}

	// Our address is private, so we test for running on GCE and for its
	// 1:1 NAT configuration. GCEPublicAddress returns a non-nil addr
	// if we are indeed running on GCE.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			ListenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			p.gce = "+gce"
			return nil
		}
	}
	ListenSpec.AddressChooser = internal.IPAddressChooser
	return nil
}

func (p *static) String() string {
	return "static profile on " + p.Platform().String()
}
