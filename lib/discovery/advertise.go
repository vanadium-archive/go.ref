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

func (d *idiscovery) advertise(ctx *context.T, session sessionId, service *discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
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

	ctx, cancel, err := d.addTask(ctx)
	if err != nil {
		return nil, err
	}

	if !d.addAd(session, ad.Service.InstanceId) {
		cancel()
		d.removeTask(ctx)
		return nil, verror.New(errAlreadyBeingAdvertised, ctx)
	}

	done := make(chan struct{})
	barrier := NewBarrier(func() {
		d.removeAd(ad.Service.InstanceId)
		d.removeTask(ctx)
		close(done)
	})
	for _, plugin := range d.plugins {
		if err := plugin.Advertise(ctx, ad, barrier.Add()); err != nil {
			cancel()
			return nil, err
		}
	}
	return done, nil
}

func (d *idiscovery) addAd(session sessionId, id string) bool {
	d.mu.Lock()
	if _, exist := d.ads[id]; exist {
		d.mu.Unlock()
		return false
	}
	d.ads[id] = session
	d.mu.Unlock()
	return true
}

func (d *idiscovery) removeAd(id string) {
	d.mu.Lock()
	delete(d.ads, id)
	d.mu.Unlock()
}
