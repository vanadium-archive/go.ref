// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
	"v.io/v23/verror"
)

var (
	errAlreadyBeingAdvertised = verror.Register(pkgPath+".errAlreadyBeingAdvertised", verror.NoRetry, "{1:}{2:} already being advertised")
	errNoInterfaceName        = verror.Register(pkgPath+".errNoInterfaceName", verror.NoRetry, "{1:}{2:} interface name not provided")
	errNotPackableAttributes  = verror.Register(pkgPath+".errNotPackableAttributes", verror.NoRetry, "{1:}{2:} attribute not packable")
	errNoAddresses            = verror.Register(pkgPath+".errNoAddress", verror.NoRetry, "{1:}{2:} address not provided")
	errNotPackableAddresses   = verror.Register(pkgPath+".errNotPackableAddresses", verror.NoRetry, "{1:}{2:} address not packable")
)

// Advertise implements discovery.Advertiser.
func (ds *ds) Advertise(ctx *context.T, service discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	if len(service.InterfaceName) == 0 {
		return nil, verror.New(errNoInterfaceName, ctx)
	}
	if len(service.Addrs) == 0 {
		return nil, verror.New(errNoAddresses, ctx)
	}
	if err := validateAttributes(service.Attrs); err != nil {
		return nil, err
	}

	if len(service.InstanceUuid) == 0 {
		service.InstanceUuid = NewInstanceUUID()
	}

	ad := Advertisement{
		ServiceUuid: NewServiceUUID(service.InterfaceName),
		Service:     service,
	}
	if err := encrypt(&ad, visibility); err != nil {
		return nil, err
	}

	ctx, cancel, err := ds.addTask(ctx)
	if err != nil {
		return nil, err
	}

	id := string(ad.Service.InstanceUuid)
	if !ds.addAd(id) {
		cancel()
		ds.removeTask(ctx)
		return nil, verror.New(errAlreadyBeingAdvertised, ctx)
	}

	done := make(chan struct{})
	barrier := NewBarrier(func() {
		ds.removeAd(id)
		ds.removeTask(ctx)
		close(done)
	})
	for _, plugin := range ds.plugins {
		if err := plugin.Advertise(ctx, ad, barrier.Add()); err != nil {
			cancel()
			return nil, err
		}
	}
	return done, nil
}

func (ds *ds) addAd(id string) bool {
	ds.mu.Lock()
	if _, exist := ds.ads[id]; exist {
		ds.mu.Unlock()
		return false
	}
	ds.ads[id] = struct{}{}
	ds.mu.Unlock()
	return true
}

func (ds *ds) removeAd(id string) {
	ds.mu.Lock()
	delete(ds.ads, id)
	ds.mu.Unlock()
}
