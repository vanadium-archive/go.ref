// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux darwin

// Package roaming implements a profile suitable for a variety of network
// configurations, including 1-1 NATs, dhcp auto-configuration, and Google
// Compute Engine.
//
// The config.Publisher mechanism is used for communicating networking
// settings to the rpc.Server implementation of the runtime and publishes
// the Settings it expects.
package roaming

import (
	"flag"
	"net"

	"v.io/x/lib/netconfig"
	"v.io/x/lib/netstate"
	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/config"
	"v.io/v23/context"
	"v.io/v23/rpc"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/profiles/internal"
	"v.io/x/ref/profiles/internal/lib/appcycle"
	"v.io/x/ref/profiles/internal/lib/websocket"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/wsh"
	"v.io/x/ref/profiles/internal/rt"
	"v.io/x/ref/services/debug/debuglib"
)

const (
	SettingsStreamName = "roaming"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfile(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}
	reservedDispatcher := debuglib.NewDispatcher(vlog.Log.LogDir, securityflag.NewAuthorizerOrDie())

	ac := appcycle.New()

	// Our address is private, so we test for running on GCE and for its
	// 1:1 NAT configuration.
	if !internal.HasPublicIP(vlog.Log) {
		if addr := internal.GCEPublicAddress(vlog.Log); addr != nil {
			listenSpec.AddressChooser = func(string, []net.Addr) ([]net.Addr, error) {
				// TODO(cnicolaou): the protocol at least should
				// be configurable, or maybe there's a profile specific
				// flag to configure both the protocol and address.
				return []net.Addr{netstate.NewNetAddr("wsh", addr.String())}, nil
			}
			runtime, ctx, shutdown, err := rt.Init(ctx, ac, nil, &listenSpec, commonFlags.RuntimeFlags(), reservedDispatcher)
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

	runtime, ctx, shutdown, err := rt.Init(ctx, ac, nil, &listenSpec, commonFlags.RuntimeFlags(), reservedDispatcher)
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
	runtime *rt.Runtime,
	ctx *context.T,
	watcher netconfig.NetConfigWatcher,
	prev netstate.AddrList,
	pubStop, cleanup <-chan struct{},
	watcherLoop chan<- struct{},
	ch chan<- config.Setting) {
	defer close(ch)

	listenSpec := runtime.GetListenSpec(ctx)

	// TODO(cnicolaou): add support for listening on multiple network addresses.

done:
	for {
		select {
		case <-watcher.Channel():
			netstate.InvalidateCache()
			cur, err := netstate.GetAccessibleIPs()
			if err != nil {
				vlog.Errorf("failed to read network state: %s", err)
				continue
			}
			removed := netstate.FindRemoved(prev, cur)
			added := netstate.FindAdded(prev, cur)
			vlog.VI(2).Infof("Previous: %d: %s", len(prev), prev)
			vlog.VI(2).Infof("Current : %d: %s", len(cur), cur)
			vlog.VI(2).Infof("Added   : %d: %s", len(added), added)
			vlog.VI(2).Infof("Removed : %d: %s", len(removed), removed)
			if len(removed) == 0 && len(added) == 0 {
				vlog.VI(2).Infof("Network event that lead to no address changes since our last 'baseline'")
				continue
			}
			if len(removed) > 0 {
				vlog.VI(2).Infof("Sending removed: %s", removed)
				ch <- rpc.NewRmAddrsSetting(removed.AsNetAddrs())
			}
			// We will always send the best currently available address
			if chosen, err := listenSpec.AddressChooser(listenSpec.Addrs[0].Protocol, cur.AsNetAddrs()); err == nil && chosen != nil {
				vlog.VI(2).Infof("Sending added and chosen: %s", chosen)
				ch <- rpc.NewAddAddrsSetting(chosen)
			} else {
				vlog.VI(2).Infof("Ignoring added %s", added)
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
