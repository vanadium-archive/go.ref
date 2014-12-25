// Package static provides a network-aware Profile that provides appropriate
// options and configuration for a variety of network configurations, including
// being behind 1-1 NATs, but without the ability to respond to dhcp changes.
package static

import (
	"flag"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/config"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/rt"

	"v.io/veyron/veyron/lib/appcycle"
	"v.io/veyron/veyron/lib/flags"
	"v.io/veyron/veyron/lib/netstate"
	"v.io/veyron/veyron/profiles/internal"
	"v.io/veyron/veyron/profiles/internal/platform"
	_ "v.io/veyron/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/veyron/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/veyron/veyron/runtimes/google/ipc/protocols/wsh"
	_ "v.io/veyron/veyron/runtimes/google/rt"
	"v.io/veyron/veyron/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "v.io/veyron/veyron/security/flag"
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
	ac  *appcycle.AppCycle
}

// New returns a new instance of a very static Profile. It can be used
// as a default by Runtime implementations, in unit tests etc.
func New() veyron2.Profile {
	return &static{}
}

func (p *static) Name() string {
	return "static" + p.gce
}

func (p *static) Runtime() (string, []veyron2.ROpt) {
	return "google", nil
}

func (*static) Platform() *veyron2.Platform {
	pstr, _ := platform.Platform()
	return pstr
}

func (p *static) Init(rt veyron2.Runtime, _ *config.Publisher) (veyron2.AppCycle, error) {
	log := rt.Logger()

	rt.ConfigureReservedName(debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie(), rt.VtraceStore()))

	lf := commonFlags.ListenFlags()
	ListenSpec = ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	p.ac = appcycle.New()

	// Our address is private, so we test for running on GCE and for its
	// 1:1 NAT configuration. GCEPublicAddress returns a non-nil addr
	// if we are indeed running on GCE.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			ListenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			p.gce = "+gce"
			return p.ac, nil
		}
	}
	ListenSpec.AddressChooser = internal.IPAddressChooser
	return p.ac, nil
}

func (p *static) Cleanup() {
	if p.ac != nil {
		p.ac.Shutdown()
	}
}

func (p *static) String() string {
	return "static profile on " + p.Platform().String()
}
