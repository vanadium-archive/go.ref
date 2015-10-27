// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package roaming

import (
	"net"

	"v.io/x/lib/netconfig"
	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/pubsub"

	"v.io/v23/context"
	"v.io/v23/rpc"
)

const (
	RoamingSetting         = "roaming"
	RoamingSettingDesc     = "pubsub stream used by the roaming RuntimeFactory"
	UpdateAddrsSetting     = "NewAddrs"
	UpdateAddrsSettingDesc = "Addresses that have been available since last change"
	RmAddrsSetting         = "RmAddrs"
	RmAddrsSettingDesc     = "Addresses that have been removed since last change"
)

// CreateRoamingStream creates a stream that monitors network configuration
// changes and publishes sebsequent Settings to reflect any changes detected.
// A synchronous shutdown function is returned.
func CreateRoamingStream(ctx *context.T, publisher *pubsub.Publisher, listenSpec rpc.ListenSpec) (func(), error) {
	ch := make(chan pubsub.Setting)
	stop, err := publisher.CreateStream(RoamingSetting, RoamingSettingDesc, ch)
	if err != nil {
		return nil, err
	}
	prev, err := netstate.GetAccessibleIPs()
	if err != nil {
		return nil, err
	}
	watcher, err := netconfig.NewNetConfigWatcher()
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-stop:
				return
			case <-watcher.Channel():
				netstate.InvalidateCache()
				cur, err := netstate.GetAccessibleIPs()
				if err != nil {
					ctx.Errorf("failed to read network state: %v", err)
					continue
				}
				removed := netstate.FindRemoved(prev, cur)
				added := netstate.FindAdded(prev, cur)
				ctx.VI(2).Infof("Previous: %d: %s", len(prev), prev)
				ctx.VI(2).Infof("Current : %d: %s", len(cur), cur)
				ctx.VI(2).Infof("Added   : %d: %s", len(added), added)
				ctx.VI(2).Infof("Removed : %d: %s", len(removed), removed)
				if len(removed) > 0 {
					ctx.VI(2).Infof("Sending removed: %s", removed)
					ch <- NewRmAddrsSetting(removed.AsNetAddrs())
				}
				if chosen, err := listenSpec.AddressChooser.ChooseAddresses(listenSpec.Addrs[0].Protocol, cur.AsNetAddrs()); err == nil && len(chosen) > 0 {
					ctx.VI(2).Infof("Sending added and chosen: %s", chosen)
					ch <- NewUpdateAddrsSetting(chosen)
				}
				prev = cur
			}
		}
	}()
	shutdown := func() {
		close(done)
		watcher.Stop()
	}
	return shutdown, nil
}

// ReadRoamingStream reads updates from the roaming RuntimeFactory. Updates are
// passed as the argument to 'update'.
// 'remove' gets passed the net.Addrs that are no longer being listened on.
// 'add' gets passed the net.Addrs that are newly being listened on.
func ReadRoamingStream(ctx *context.T, pub *pubsub.Publisher, remove, add func([]net.Addr)) {
	ch := make(chan pubsub.Setting, 10)
	_, err := pub.ForkStream(RoamingSetting, ch)
	if err != nil {
		ctx.Errorf("error forking stream:", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			if err := pub.CloseFork(RoamingSetting, ch); err == nil {
				drain(ch)
			}
			return
		case setting := <-ch:
			if setting == nil {
				continue
			}
			addrs, ok := setting.Value().([]net.Addr)
			if !ok {
				ctx.Errorf("unhandled setting type %T", addrs)
				continue
			}
			switch setting.Name() {
			case UpdateAddrsSetting:
				add(addrs)
			case RmAddrsSetting:
				remove(addrs)
			}
		}
	}
}

func drain(ch chan pubsub.Setting) {
	for {
		select {
		case <-ch:
		default:
			close(ch)
			return
		}
	}
}

// NewUpdateAddrsSetting creates the Setting to be sent to Listen to inform
// it of all addresses that habe become available since the last change.
func NewUpdateAddrsSetting(a []net.Addr) pubsub.Setting {
	return pubsub.NewAny(UpdateAddrsSetting, UpdateAddrsSettingDesc, a)
}

// NewRmAddrsSetting creates the Setting to be sent to Listen to inform
// it of addresses that are no longer available.
func NewRmAddrsSetting(a []net.Addr) pubsub.Setting {
	return pubsub.NewAny(RmAddrsSetting, RmAddrsSettingDesc, a)
}
