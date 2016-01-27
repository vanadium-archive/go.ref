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
	errInvalidService         = verror.Register(pkgPath+"errInvalidService", verror.NoRetry, "{1:}{2:} service not valid{:_}")
)

// Advertise implements discovery.Advertiser.
func (ds *ds) Advertise(ctx *context.T, service *discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	if len(service.InstanceId) == 0 {
		var err error
		if service.InstanceId, err = newInstanceId(); err != nil {
			return nil, err
		}
	}
	if err := validateService(service); err != nil {
		return nil, verror.New(errInvalidService, ctx, err)
	}

	ad := Advertisement{Service: copyService(service)}
	if err := encrypt(&ad, visibility); err != nil {
		return nil, err
	}
	hashAdvertisement(&ad)

	ctx, cancel, err := ds.addTask(ctx)
	if err != nil {
		return nil, err
	}

	if !ds.addAd(ad.Service.InstanceId) {
		cancel()
		ds.removeTask(ctx)
		return nil, verror.New(errAlreadyBeingAdvertised, ctx)
	}

	done := make(chan struct{})
	barrier := NewBarrier(func() {
		ds.removeAd(ad.Service.InstanceId)
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
