// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"time"

	"v.io/v23/context"
	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
)

const (
	// TTL for scanned advertisement. If we do not see the advertisement again
	// during that period, we send a "Lost" notification.
	defaultTTL = 90 * time.Second
)

type plugin struct {
	advertiser *advertiser
	scanner    *scanner

	adStopper *idiscovery.Trigger
}

func (p *plugin) Advertise(ctx *context.T, adinfo *idiscovery.AdInfo, done func()) (err error) {
	if err := p.advertiser.addAd(adinfo); err != nil {
		done()
		return err
	}
	stop := func() {
		p.advertiser.removeAd(adinfo)
		done()
	}
	p.adStopper.Add(stop, ctx.Done())
	return nil
}

func (p *plugin) Scan(ctx *context.T, interfaceName string, ch chan<- *idiscovery.AdInfo, done func()) error {
	go func() {
		defer done()

		listener := p.scanner.addListener(interfaceName)
		defer p.scanner.removeListener(interfaceName, listener)

		seen := make(map[discovery.AdId]*idiscovery.AdInfo)
		for {
			select {
			case adinfo := <-listener:
				if adinfo.Lost {
					delete(seen, adinfo.Ad.Id)
				} else {
					prev := seen[adinfo.Ad.Id]
					if prev != nil && (prev.Hash == adinfo.Hash || prev.TimestampNs >= adinfo.TimestampNs) {
						continue
					}
					seen[adinfo.Ad.Id] = adinfo
				}
				copied := *adinfo
				select {
				case ch <- &copied:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (p *plugin) Close() {
	p.scanner.shutdown()
}

// New returns a new BLE plugin instance with default ttl (90s).
//
// TODO(jhahn): Rename to New() once we remove old codes.
func New(ctx *context.T, host string) (idiscovery.Plugin, error) {
	return newWithTTL(ctx, host, defaultTTL)
}

func newWithTTL(ctx *context.T, host string, ttl time.Duration) (idiscovery.Plugin, error) {
	driver, err := driverFactory(ctx, host)
	if err != nil {
		return nil, err
	}
	p := &plugin{
		advertiser: newAdvertiser(ctx, driver),
		scanner:    newScanner(ctx, driver, ttl),
		adStopper:  idiscovery.NewTrigger(),
	}
	return p, nil
}
