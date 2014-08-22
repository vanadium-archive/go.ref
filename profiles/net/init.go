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

	"veyron/profiles"

	// TODO(cnicolaou): move this to profiles/internal
	"veyron/runtimes/google/lib/netconfig"
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
	listen_protocol string
	listen_addr     config.IPFlag
)

func init() {
	flag.StringVar(&listen_protocol, "veyron.protocol", "tcp4", "protocol to listen with")
	flag.Var(&listen_addr, "veyron.address", "address to listen on")
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
	go monitorAndPublishNetworkSettings(rt, stop, ch, listen_protocol, listen_addr.IP.String())
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
	listenProtocol string, listenSpec string) {
	defer close(ch)

	log := rt.Logger()
	prev4, _, prevAddr := firstUsableIPv4()
	// prevAddr may be nil if we are currently offline.

	publishInitialSettings(ch, listenProtocol, listenSpec, prevAddr)

	// Start the dhcp watcher.
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		log.VI(1).Infof("Failed to get new config watcher: %s", err)
		<-stop
		return
	}

	for {
		select {
		case <-watcher.Channel():
			cur4, _, _ := ipState()
			added := findAdded(prev4, cur4)
			ifc, newAddr := added.first()
			log.VI(1).Infof("new address found: %s:%s", ifc, newAddr)
			removed := findRemoved(prev4, cur4)
			if prevAddr == nil || (removed.has(prevAddr) && newAddr != nil) {
				log.VI(1).Infof("address change from %s to %s:%s",
					prevAddr, ifc, newAddr)
				ch <- config.NewIP(AddPublishAddressSetting, "new dhcp address to publish", newAddr)
				ch <- config.NewIP(RmPublishAddressSetting, "remove address", prevAddr)
				prevAddr = newAddr
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
