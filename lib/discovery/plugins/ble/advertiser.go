// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"fmt"
	"sync"
	"time"

	"v.io/v23/context"

	idiscovery "v.io/x/ref/lib/discovery"
)

type advertiser struct {
	driver Driver

	mu        sync.Mutex
	adRecords map[string]*adRecord // GUARDED_BY(mu)
}

type adRecord struct {
	uuid   idiscovery.Uuid
	expiry time.Time
}

const (
	uuidGcDelay = 10 * time.Minute
)

func (a *advertiser) addAd(adinfo *idiscovery.AdInfo) error {
	cs, err := encodeAdInfo(adinfo)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.gcLocked()

	rec := a.adRecords[adinfo.Ad.InterfaceName]
	switch {
	case rec == nil:
		rec = &adRecord{uuid: newServiceUuid(adinfo.Ad.InterfaceName)}
		a.adRecords[adinfo.Ad.InterfaceName] = rec
	case rec.expiry.IsZero():
		// We do not support multiple instances of a same service for now.
		return fmt.Errorf("service already being advertised: %s", adinfo.Ad.InterfaceName)
	default:
		// The previous advertisement has been stopped recently. Use a toggled version
		// of the previous uuid to avoid a new advertisement from being deduped by cache.
		toggleServiceUuid(rec.uuid)
		rec.expiry = time.Time{}
	}

	err = a.driver.AddService(rec.uuid.String(), cs)
	if err != nil {
		rec.expiry = time.Now().Add(uuidGcDelay)
	}
	return err
}

func (a *advertiser) removeAd(interfaceName string) {
	a.mu.Lock()
	if rec := a.adRecords[interfaceName]; rec != nil {
		a.driver.RemoveService(rec.uuid.String())
		rec.expiry = time.Now().Add(uuidGcDelay)
	}
	a.gcLocked()
	a.mu.Unlock()
}

func (a *advertiser) gcLocked() {
	// Instead of asynchronous gc, we purge old entries in every call to addAd or removeAd
	// for simplicity. We do not worry about purging all old entries since there will be
	// only a handful of ads in practice.
	now := time.Now()
	for interfaceName, rec := range a.adRecords {
		if rec.expiry.IsZero() || rec.expiry.After(now) {
			continue
		}
		delete(a.adRecords, interfaceName)
	}
}

func newAdvertiser(ctx *context.T, driver Driver) *advertiser {
	return &advertiser{
		driver:    driver,
		adRecords: make(map[string]*adRecord),
	}
}
