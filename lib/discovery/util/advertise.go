// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"

	idiscovery "v.io/x/ref/lib/discovery"
)

// AdvertiseServer advertises the server with the given service. It uses the
// server's endpoints and the given suffix as the service addresses, and the
// addresses will be updated automatically when the underlying network are
// changed. Advertising will continue until the context is canceled or exceeds
// its deadline and the returned channel will be closed when it stops.
func AdvertiseServer(ctx *context.T, server rpc.Server, suffix string, service discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	// Assign the instance UUID if not set in order to keep the same instance UUID
	// when the advertisement is updated.
	if len(service.InstanceUuid) == 0 {
		service.InstanceUuid = idiscovery.NewInstanceUUID()
	}

	watcher := make(chan rpc.NetworkChange, 3)
	server.WatchNetwork(watcher)

	stop, err := advertise(ctx, service, server.Status().Endpoints, suffix, visibility)
	if err != nil {
		server.UnwatchNetwork(watcher)
		close(watcher)
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-watcher:
				if stop != nil {
					stop() // Stop the previous advertisement.
				}
				stop, err = advertise(ctx, service, server.Status().Endpoints, suffix, visibility)
				if err != nil {
					ctx.Error(err)
				}
			case <-ctx.Done():
				server.UnwatchNetwork(watcher)
				close(watcher)
				close(done)
				return
			}
		}
	}()

	return done, nil
}

func advertise(ctx *context.T, service discovery.Service, eps []naming.Endpoint, suffix string, visibility []security.BlessingPattern) (func(), error) {
	service.Addrs = make([]string, len(eps))
	for i, ep := range eps {
		service.Addrs[i] = naming.JoinAddressName(ep.Name(), suffix)
	}
	ds := v23.GetDiscovery(ctx)
	ctx, cancel := context.WithCancel(ctx)
	done, err := ds.Advertise(ctx, service, visibility)
	if err != nil {
		cancel()
		return nil, err
	}
	stop := func() {
		cancel()
		<-done
	}
	return stop, nil
}
