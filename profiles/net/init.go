// +build linux darwin

// Package net provides a network-aware Profile that provides appropriate
// options and configuration for a variety of network configurations, including
// being behind 1-1 NATs, using dhcp and auto-configuration for being on
// Google Compute Engine.
//
// The config.Publisher mechanism is used for communicating networking
// settings to other components of the application. The Settings stream
// is called "net" (net.StreamName) and the Settings sent over it are
// those defined by the ipc package.
// TODO(cnicolaou): define the settings in the ipc package, not here.
package net

import (
	"flag"
	"net"

	"veyron2"
	"veyron2/config"
	"veyron2/rt"

	"veyron/lib/netconfig"
	"veyron/profiles"
)

const (
	StreamName = "net"

	// TODO(cnicolaou): these will eventually be defined in the veyron2/ipc
	// package.
	ProtocolSetting          = "Protocol"
	ListenSpecSetting        = "ListenSpec"
	AddPublishAddressSetting = "AddPublishAddr"
	RmPublishAddressSetting  = "RmPublishAddr"
)

var (
	listenProtocolFlag = config.TCPProtocolFlag{"tcp4"}
	listenSpecFlag     = config.IPHostPortFlag{Port: ":0"}
)

func init() {
	flag.Var(&listenProtocolFlag, "veyron.tcpprotocol", "protocol to listen with")
	flag.Var(&listenSpecFlag, "veyron.tcpaddress", "address to listen on")
	rt.RegisterProfile(New())
}

type profile struct{}

func New() veyron2.Profile {
	return &profile{}
}

func (p *profile) Platform() *veyron2.Platform {
	platform, _ := profiles.Platform()
	return platform
}

func (p *profile) Name() string {
	return "net"
}

func (p *profile) Runtime() string {
	return ""
}

func (p *profile) String() string {
	return "net " + p.Platform().String()
}

func (p *profile) Init(rt veyron2.Runtime, publisher *config.Publisher) {
	log := rt.Logger()

	// TODO(cnicolaou): figure out the correct heuristics for using IPv6.
	_, _, first := firstUsableIPv4()
	if first == nil {
		log.Infof("failed to find any usable IP addresses at startup")
	}
	public := publicIP(first)

	// We now know that there is an IP address to listen on, and whether
	// it's public or private.

	// Our address is private, so we test for running on GCE
	// and for its 1:1 NAT configuration. handleGCE returns true
	// if we are indeed running on GCE.
	if !public && handleGCE(rt, publisher) {
		return
	}

	// Create stream in Init function to avoid a race between any
	// goroutines started here and consumers started after Init returns.
	ch := make(chan config.Setting)
	stop, err := publisher.CreateStream(StreamName, "network configuration", ch)
	if err != nil {
		log.Errorf("failed to create publisher: %s", err)
		return
	}
	go monitorAndPublishNetworkSettings(rt, stop, ch, listenProtocolFlag.Protocol, listenSpecFlag)
}

func publishInitialSettings(ch chan<- config.Setting, protocol, listenSpec string, addr net.IP) {
	for _, setting := range []config.Setting{
		config.NewString(ProtocolSetting, "protocol to listen with", protocol),
		config.NewString(ListenSpecSetting, "address spec to listen on", listenSpec),
		config.NewIP(AddPublishAddressSetting, "address to publish", addr),
	} {
		ch <- setting
	}
}

// monitorNetworkSettings will publish initial Settings and then
// monitor network configuration changes and publish subsequent
// Settings to reflect any changes detected. It will never publish an
// RmPublishAddressSetting without first sending an AddPublishAddressSetting.
func monitorAndPublishNetworkSettings(rt veyron2.Runtime, stop <-chan struct{},
	ch chan<- config.Setting,
	listenProtocol string, listenSpec config.IPHostPortFlag) {
	defer close(ch)

	log := rt.Logger()

	prev4, _, prevAddr := firstUsableIPv4()
	// TODO(cnicolaou): check that prev4 is one of the IPs that a hostname
	// resolved to, if we used a hostname in the listenspec.

	// prevAddr may be nil if we are currently offline.

	log.Infof("Initial Settings: %s %s publish %s", listenProtocol, listenSpec, prevAddr)
	publishInitialSettings(ch, listenProtocol, listenSpec.String(), prevAddr)

	// Start the dhcp watcher.
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		log.VI(2).Infof("Failed to get new config watcher: %s", err)
		<-stop
		return
	}

	for {
		select {
		case <-watcher.Channel():
			cur4, _, _ := ipState()
			removed := findRemoved(prev4, cur4)
			added := findAdded(prev4, cur4)
			log.VI(2).Infof("Previous: %d: %s", len(prev4), prev4)
			log.VI(2).Infof("Current : %d: %s", len(cur4), cur4)
			log.VI(2).Infof("Added   : %d: %s", len(added), added)
			log.VI(2).Infof("Removed : %d: %s", len(removed), removed)
			if len(removed) == 0 && len(added) == 0 {
				log.VI(2).Infof("Network event that lead to no address changes since our last 'baseline'")
				continue
			}
			if len(added) == 0 {
				// Nothing has been added, but something has been removed.
				if !removed.has(prevAddr) {
					log.VI(1).Infof("An address we were not using was removed")
					continue
				}
				log.Infof("Publish address is no longer available: %s", prevAddr)
				// Our currently published address has been removed and
				// a new one has not been added.
				// TODO(cnicolaou): look for a new address to use right now.
				prevAddr = nil
				continue
			}
			log.Infof("At least one address has been added and zero or more removed")
			// something has been added, and maybe something has been removed
			ifc, newAddr := added.first()
			if newAddr != nil && (prevAddr == nil || removed.has(prevAddr)) {
				log.Infof("Address being changed from %s to %s:%s",
					prevAddr, ifc, newAddr)
				ch <- config.NewIP(AddPublishAddressSetting, "new dhcp address to publish", newAddr)
				ch <- config.NewIP(RmPublishAddressSetting, "remove address", prevAddr)
				prevAddr = newAddr
				prev4 = cur4
				log.VI(2).Infof("Network baseline set to %s", cur4)
			}
		case <-stop:
			return
		}
	}
}

func firstUsableIPv4() (ipAndIf, string, net.IP) {
	v4, _, _ := publicIPState()
	if v4.empty() {
		v4, _, _ = ipState()
	}
	ifc, first := v4.first()
	return v4, ifc, first
}
