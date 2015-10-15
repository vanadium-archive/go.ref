// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux darwin

// Package roaming implements a RuntimeFactory suitable for a variety of network
// configurations, including 1-1 NATs, dhcp auto-configuration, and Google
// Compute Engine.
//
// The pubsub.Publisher mechanism is used for communicating networking
// settings to the rpc.Server implementation of the runtime and publishes
// the Settings it expects.
package roaming

import (
	"flag"

	"v.io/x/lib/netconfig"
	"v.io/x/lib/netstate"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc"

	"v.io/x/ref/internal/logger"
	dfactory "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/runtime/internal"
	_ "v.io/x/ref/runtime/internal/flow/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/flow/protocols/ws"
	_ "v.io/x/ref/runtime/internal/flow/protocols/wsh"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	"v.io/x/ref/runtime/internal/lib/websocket"
	"v.io/x/ref/runtime/internal/lib/xwebsocket"
	irpc "v.io/x/ref/runtime/internal/rpc"
	"v.io/x/ref/runtime/internal/rt"
	"v.io/x/ref/services/debug/debuglib"

	// TODO(suharshs): Remove these once we switch to the flow protocols.
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh"
)

const (
	SettingsStreamName = "roaming"
	SettingsStreamDesc = "pubsub stream used by the roaming RuntimeFactory"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterRuntimeFactory(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener)
	flow.RegisterUnknownProtocol("wsh", xwebsocket.WSH{})
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlagsAndConfigureGlobalLogger(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	ac := appcycle.New()
	discovery, err := dfactory.New()
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs(lf.Addrs),
		Proxy:          lf.Proxy,
		AddressChooser: internal.NewAddressChooser(logger.Global()),
	}
	reservedDispatcher := debuglib.NewDispatcher(securityflag.NewAuthorizerOrDie())

	ishutdown := func() {
		ac.Shutdown()
		discovery.Close()
	}

	publisher := pubsub.NewPublisher()

	// Create stream in Init function to avoid a race between any
	// goroutines started here and consumers started after Init returns.
	ch := make(chan pubsub.Setting)
	// TODO(cnicolaou): use stop to shutdown this stream when the RuntimeFactory shutdowns.
	stop, err := publisher.CreateStream(SettingsStreamName, SettingsStreamDesc, ch)
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}

	prev, err := netstate.GetAccessibleIPs()
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}

	// Start the dhcp watcher.
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}

	cleanupCh := make(chan struct{})
	watcherCh := make(chan struct{})

	runtime, ctx, shutdown, err := rt.Init(ctx, ac, discovery, nil, &listenSpec, publisher, SettingsStreamName, commonFlags.RuntimeFlags(), reservedDispatcher)
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}

	go monitorNetworkSettingsX(runtime, ctx, watcher, prev, stop, cleanupCh, watcherCh, ch)
	runtimeFactoryShutdown := func() {
		close(cleanupCh)
		ishutdown()
		shutdown()
		<-watcherCh
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
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
	ch chan<- pubsub.Setting) {
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
				ctx.Errorf("failed to read network state: %s", err)
				continue
			}
			removed := netstate.FindRemoved(prev, cur)
			added := netstate.FindAdded(prev, cur)
			ctx.VI(2).Infof("Previous: %d: %s", len(prev), prev)
			ctx.VI(2).Infof("Current : %d: %s", len(cur), cur)
			ctx.VI(2).Infof("Added   : %d: %s", len(added), added)
			ctx.VI(2).Infof("Removed : %d: %s", len(removed), removed)
			if len(removed) == 0 && len(added) == 0 {
				ctx.VI(2).Infof("Network event that lead to no address changes since our last 'baseline'")
				continue
			}
			if len(removed) > 0 {
				ctx.VI(2).Infof("Sending removed: %s", removed)
				ch <- irpc.NewRmAddrsSetting(removed.AsNetAddrs())
			}
			// We will always send the best currently available address
			if chosen, err := listenSpec.AddressChooser.ChooseAddresses(listenSpec.Addrs[0].Protocol, cur.AsNetAddrs()); err == nil && chosen != nil {
				ctx.VI(2).Infof("Sending added and chosen: %s", chosen)
				ch <- irpc.NewNewAddrsSetting(chosen)
			} else {
				ctx.VI(2).Infof("Ignoring added %s", added)
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
