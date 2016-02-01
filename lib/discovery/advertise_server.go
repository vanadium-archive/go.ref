// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

// AdvertiseServer advertises the server with the given service. It uses the
// server's endpoints and the given suffix as the service addresses, and the
// addresses will be updated automatically when the underlying network are
// changed. Advertising will continue until the context is canceled or exceeds
// its deadline and the returned channel will be closed when it stops.
//
// If a discovery instance is not provided, this will create a new one.
func AdvertiseServer(ctx *context.T, d discovery.T, server rpc.Server, suffix string, service *discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	if d == nil {
		var err error
		d, err = v23.NewDiscovery(ctx)
		if err != nil {
			return nil, err
		}
	}
	// Take a copy of the service to avoid any interference from changes in user side.
	copiedService := copyService(service)

	eps, valid := getEndpoints(server)
	stop, err := advertiseServer(ctx, d, &copiedService, eps, suffix, visibility)
	if err != nil {
		return nil, err
	}

	// Copy back the instance id.
	service.InstanceId = copiedService.InstanceId

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-valid:
				if stop != nil {
					stop() // Stop the previous advertisement.
				}
				eps, valid = getEndpoints(server)
				stop, err = advertiseServer(ctx, d, &copiedService, eps, suffix, visibility)
				if err != nil {
					ctx.Error(err)
				}
			case <-ctx.Done():
				close(done)
				return
			}
		}
	}()

	return done, nil
}

func advertiseServer(ctx *context.T, d discovery.T, service *discovery.Service, eps []naming.Endpoint, suffix string, visibility []security.BlessingPattern) (func(), error) {
	service.Addrs = make([]string, len(eps))
	for i, ep := range eps {
		service.Addrs[i] = naming.JoinAddressName(ep.Name(), suffix)
	}
	ctx, cancel := context.WithCancel(ctx)
	done, err := d.Advertise(ctx, service, visibility)
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

func getEndpoints(server rpc.Server) ([]naming.Endpoint, <-chan struct{}) {
	status := server.Status()
	return status.Endpoints, status.Valid
}
