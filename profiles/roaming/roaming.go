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
	"v.io/core/veyron2"
	"v.io/core/veyron2/config"
	"v.io/core/veyron2/ipc"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/lib/netconfig"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/profiles/internal"
	"v.io/core/veyron/profiles/internal/platform"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	_ "v.io/core/veyron/runtimes/google/rt"
	"v.io/core/veyron/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "v.io/core/veyron/security/flag"
)

var (
	// ListenSpec is an initialized instance of ipc.ListenSpec that can
	// be used with ipc.Listen.
	ListenSpec ipc.ListenSpec
)

type profile struct {
	gce                  string
	ac                   *appcycle.AppCycle
	cleanupCh, watcherCh chan struct{}
}

func New() veyron2.Profile {
	return &profile{}
}

func (p *profile) Platform() *veyron2.Platform {
	pstr, _ := platform.Platform()
	return pstr
}

func (p *profile) Name() string {
	return "roaming" + p.gce
}

func (p *profile) Runtime() (string, []veyron2.ROpt) {
	return veyron2.GoogleRuntimeName, nil
}

func (p *profile) String() string {
	return p.Name() + " " + p.Platform().String()
}

func (p *profile) Init(rt veyron2.Runtime, publisher *config.Publisher) (veyron2.AppCycle, error) {
	log := veyron2.GetLogger(rt.NewContext())

	rt.ConfigureReservedName(debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie()))

	lf := commonFlags.ListenFlags()
	ListenSpec = ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	p.ac = appcycle.New()

	// Our address is private, so we test for running on GCE and for its
	// 1:1 NAT configuration.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			ListenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			p.gce = "+gce"
			return p.ac, nil
		}
	}

	// Create stream in Init function to avoid a race between any
	// goroutines started here and consumers started after Init returns.
	ch := make(chan config.Setting)
	stop, err := publisher.CreateStream(SettingsStreamName, SettingsStreamName, ch)
	if err != nil {
		log.Errorf("failed to create publisher: %s", err)
		p.ac.Shutdown()
		return nil, err
	}

	prev, err := netstate.GetAccessibleIPs()
	if err != nil {
		log.VI(2).Infof("failed to determine network state")
		p.ac.Shutdown()
		return nil, err
	}

	// Start the dhcp watcher.
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		log.VI(2).Infof("Failed to get new config watcher: %s", err)
		p.ac.Shutdown()
		return nil, err
	}

	p.cleanupCh = make(chan struct{})
	p.watcherCh = make(chan struct{})

	ListenSpec.StreamPublisher = publisher
	ListenSpec.StreamName = SettingsStreamName
	ListenSpec.AddressChooser = internal.IPAddressChooser

	go monitorNetworkSettings(rt, watcher, prev, stop, p.cleanupCh, p.watcherCh, ch, ListenSpec)
	return p.ac, nil
}

func (p *profile) Cleanup() {
	if p.cleanupCh != nil {
		close(p.cleanupCh)
	}
	if p.ac != nil {
		p.ac.Shutdown()
	}
	if p.watcherCh != nil {
		<-p.watcherCh
	}
}

// monitorNetworkSettings will monitor network configuration changes and
// publish subsequent Settings to reflect any changes detected.
func monitorNetworkSettings(rt veyron2.Runtime, watcher netconfig.NetConfigWatcher, prev netstate.AddrList, pubStop, cleanup <-chan struct{},
	watcherLoop chan<- struct{}, ch chan<- config.Setting, listenSpec ipc.ListenSpec) {
	defer close(ch)

	log := veyron2.GetLogger(rt.NewContext())

	// TODO(cnicolaou): add support for listening on multiple network addresses.

done:
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
			if chosen, err := listenSpec.AddressChooser(listenSpec.Addrs[0].Protocol, cur); err == nil && chosen != nil {
				ch <- ipc.NewAddAddrsSetting(chosen)
			}
			prev = cur
		case <-cleanup:
			break done
		case <-pubStop:
			goto done
		}
	}
	watcher.Stop()
	close(watcherLoop)
}
