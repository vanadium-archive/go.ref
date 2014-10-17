// +build linux darwin

// Package roaming provides a network-aware Profile that provides appropriate
// options and configuration for a variety of network configurations, including
// being behind 1-1 NATs, using dhcp and auto-configuration for being on
// Google Compute Engine.
//
// The config.Publisher mechanism is used for communicating networking
// settings to the ipc.Server implementation of the runtime and publishes
// the Settings it expects.
package roaming

import (
	"flag"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/flags"
	"veyron.io/veyron/veyron/lib/netconfig"
	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/profiles/internal"
)

const (
	SettingsStreamName = "roaming"
)

var (
	listenProtocolFlag = flags.TCPProtocolFlag{"tcp"}
	listenAddressFlag  = flags.IPHostPortFlag{Port: "0"}
	listenProxyFlag    string

	// ListenSpec is an initialized instance of ipc.ListenSpec that can
	// be used with ipc.Listen.
	ListenSpec *ipc.ListenSpec
)

func init() {
	flag.Var(&listenProtocolFlag, "veyron.tcp.protocol", "protocol to listen with")
	flag.Var(&listenAddressFlag, "veyron.tcp.address", "address to listen on")
	flag.StringVar(&listenProxyFlag, "veyron.proxy", "", "object name of proxy service to use to export services across network boundaries")
	rt.RegisterProfile(New())
}

type profile struct {
	gce string
}

func New() veyron2.Profile {
	return &profile{}
}

func (p *profile) Platform() *veyron2.Platform {
	platform, _ := profiles.Platform()
	return platform
}

func (p *profile) Name() string {
	return "roaming" + p.gce
}

func (p *profile) Runtime() string {
	return veyron2.GoogleRuntimeName
}

func (p *profile) String() string {
	return p.Name() + " " + p.Platform().String()
}

func (p *profile) Init(rt veyron2.Runtime, publisher *config.Publisher) error {
	log := rt.Logger()

	ListenSpec = &ipc.ListenSpec{
		Protocol: listenProtocolFlag.Protocol,
		Address:  listenAddressFlag.String(),
		Proxy:    listenProxyFlag,
	}

	// Our address is private, so we test for running on GCE and for its
	// 1:1 NAT configuration.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			ListenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			p.gce = "+gce"
			return nil
		}
	}

	// Create stream in Init function to avoid a race between any
	// goroutines started here and consumers started after Init returns.
	ch := make(chan config.Setting)
	stop, err := publisher.CreateStream(SettingsStreamName, SettingsStreamName, ch)
	if err != nil {
		log.Errorf("failed to create publisher: %s", err)
		return err
	}

	ListenSpec.StreamPublisher = publisher
	ListenSpec.StreamName = SettingsStreamName
	ListenSpec.AddressChooser = internal.IPAddressChooser
	go monitorNetworkSettings(rt, stop, ch, ListenSpec)
	return nil
}

// monitorNetworkSettings will monitor network configuration changes and
// publish subsequent Settings to reflect any changes detected.
func monitorNetworkSettings(rt veyron2.Runtime, stop <-chan struct{},
	ch chan<- config.Setting, listenSpec *ipc.ListenSpec) {
	defer close(ch)

	log := rt.Logger()
	prev, err := netstate.GetAccessibleIPs()
	if err != nil {
		// TODO(cnicolaou): add support for shutting down profiles
		//<-stop
		log.VI(2).Infof("failed to determine network state")
		return
	}

	// Start the dhcp watcher.
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		log.VI(2).Infof("Failed to get new config watcher: %s", err)
		// TODO(cnicolaou): add support for shutting down profiles
		//<-stop
		return
	}

	for {
		select {
		case <-watcher.Channel():
			cur, err := netstate.GetAccessibleIPs()
			if err != nil {
				log.Errorf("failed to read network state: %s", err)
				continue
			}
			removed := netstate.FindRemoved(prev, cur)
			added := netstate.FindAdded(prev, cur)
			log.VI(2).Infof("Previous: %d: %s", len(prev), prev)
			log.VI(2).Infof("Current : %d: %s", len(cur), cur)
			log.VI(2).Infof("Added   : %d: %s", len(added), added)
			log.VI(2).Infof("Removed : %d: %s", len(removed), removed)
			if len(removed) == 0 && len(added) == 0 {
				log.VI(2).Infof("Network event that lead to no address changes since our last 'baseline'")
				continue
			}
			if len(removed) > 0 {
				log.VI(2).Infof("Sending removed: %s", removed)
				ch <- ipc.NewRmAddrsSetting(removed)
			}
			// We will always send the best currently available address
			if chosen, err := listenSpec.AddressChooser(listenSpec.Protocol, cur); err == nil && chosen != nil {
				ch <- ipc.NewAddAddrsSetting(chosen)
			}
			prev = cur
			// TODO(cnicolaou): add support for shutting down profiles.
			//case <-stop:
			//	return
		}
	}
}
