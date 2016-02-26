// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
)

func (d *idiscovery) advertise(ctx *context.T, session sessionId, ad *discovery.Advertisement, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	if !ad.Id.IsValid() {
		var err error
		if ad.Id, err = discovery.NewAdId(); err != nil {
			return nil, err
		}
	}
	if err := validateAd(ad); err != nil {
		return nil, NewErrBadAdvertisement(ctx, err)
	}

	adinfo := &AdInfo{Ad: *ad}
	if err := encrypt(ctx, adinfo, visibility); err != nil {
		return nil, err
	}
	HashAd(adinfo)

	ctx, cancel, err := d.addTask(ctx)
	if err != nil {
		return nil, err
	}

	if !d.addAd(session, adinfo) {
		cancel()
		d.removeTask(ctx)
		return nil, NewErrAlreadyBeingAdvertised(ctx, adinfo.Ad.Id)
	}

	done := make(chan struct{})
	barrier := NewBarrier(func() {
		d.removeAd(adinfo)
		d.removeTask(ctx)
		close(done)
	})
	for _, plugin := range d.plugins {
		if err := plugin.Advertise(ctx, adinfo, barrier.Add()); err != nil {
			cancel()
			return nil, err
		}
	}
	return done, nil
}

func (d *idiscovery) addAd(session sessionId, adinfo *AdInfo) bool {
	d.mu.Lock()
	if _, exist := d.ads[adinfo.Ad.Id]; exist {
		d.mu.Unlock()
		return false
	}
	d.ads[adinfo.Ad.Id] = session
	d.mu.Unlock()
	return true
}

func (d *idiscovery) removeAd(adinfo *AdInfo) {
	d.mu.Lock()
	delete(d.ads, adinfo.Ad.Id)
	d.mu.Unlock()
}
