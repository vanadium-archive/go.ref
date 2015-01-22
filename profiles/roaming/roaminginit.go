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
	"os"

	"v.io/core/veyron2"
	"v.io/core/veyron2/config"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/netconfig"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/lib/websocket"
	"v.io/core/veyron/profiles/internal"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	grt "v.io/core/veyron/runtimes/google/rt"
	"v.io/core/veyron/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "v.io/core/veyron/security/flag"
)

const (
	SettingsStreamName = "roaming"
)

var (
	commonFlags *flags.Flags
)

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Listen)
	veyron2.RegisterProfileInit(Init)
	stream.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	log := vlog.Log
	reservedDispatcher := debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie())

	commonFlags.Parse(os.Args[1:], nil)
	lf := commonFlags.ListenFlags()
	listenSpec := ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	ac := appcycle.New()

	// Our address is private, so we test for running on GCE and for its
	// 1:1 NAT configuration.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			listenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, reservedDispatcher)
			if err != nil {
				return nil, nil, shutdown, err
			}
			profileShutdown := func() {
				ac.Shutdown()
				shutdown()
			}
			return runtime, ctx, profileShutdown, nil
		}
	}

	publisher := config.NewPublisher()

	// Create stream in Init function to avoid a race between any
	// goroutines started here and consumers started after Init returns.
	ch := make(chan config.Setting)
	stop, err := publisher.CreateStream(SettingsStreamName, SettingsStreamName, ch)
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	prev, err := netstate.GetAccessibleIPs()
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	// Start the dhcp watcher.
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	cleanupCh := make(chan struct{})
	watcherCh := make(chan struct{})

	listenSpec.StreamPublisher = publisher
	listenSpec.StreamName = SettingsStreamName
	listenSpec.AddressChooser = internal.IPAddressChooser

	runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, reservedDispatcher)
	if err != nil {
		return nil, nil, shutdown, err
	}

	go monitorNetworkSettingsX(runtime, ctx, watcher, prev, stop, cleanupCh, watcherCh, ch)
	profileShutdown := func() {
		close(cleanupCh)
		ac.Shutdown()
		shutdown()
		<-watcherCh
	}
	return runtime, ctx, profileShutdown, nil
}

// monitorNetworkSettings will monitor network configuration changes and
// publish subsequent Settings to reflect any changes detected.
func monitorNetworkSettingsX(
	runtime *grt.RuntimeX,
	ctx *context.T,
	watcher netconfig.NetConfigWatcher,
	prev netstate.AddrList,
	pubStop, cleanup <-chan struct{},
	watcherLoop chan<- struct{},
	ch chan<- config.Setting) {
	defer close(ch)

	log := runtime.GetLogger(ctx)
	listenSpec := runtime.GetListenSpec(ctx)

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
