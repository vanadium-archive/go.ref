// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"fmt"
	"reflect"
	"time"

	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"
)

const scanInterval = 90 * time.Second

func (d *gdiscovery) Scan(ctx *context.T, query string) (<-chan discovery.Update, error) {
	matcher, target, err := newMatcher(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(target) == 0 {
		target = "*"
	}

	updateCh := make(chan discovery.Update, 10)
	go func() {
		defer close(updateCh)

		var prevFound map[string]*discovery.Service
		for {
			found, err := d.doScan(ctx, target, matcher)
			if err != nil {
				ctx.Error(err)
			} else {
				mergeAdvertisements(ctx, prevFound, found, updateCh)
				prevFound = found
			}

			select {
			case <-d.clock.After(scanInterval):
			case <-ctx.Done():
				return
			}
		}
	}()
	return updateCh, nil
}

func (d *gdiscovery) doScan(ctx *context.T, target string, matcher matcher) (map[string]*discovery.Service, error) {
	scanCh, err := d.ns.Glob(ctx, target)
	if err != nil {
		return nil, err
	}
	defer func() {
		for range scanCh {
		}
	}()

	found := make(map[string]*discovery.Service)
	for {
		select {
		case glob, ok := <-scanCh:
			if !ok {
				return found, nil
			}
			service, err := convToAdvertisement(glob)
			if err != nil {
				ctx.Error(err)
				continue
			}
			if d.hasAd(service.InstanceId) {
				continue
			}
			if matcher.match(service) {
				found[service.InstanceId] = service
			}
		case <-ctx.Done():
			return nil, nil
		}
	}
}

func (d *gdiscovery) hasAd(id string) bool {
	d.mu.Lock()
	_, ok := d.ads[id]
	d.mu.Unlock()
	return ok
}

func convToAdvertisement(glob naming.GlobReply) (*discovery.Service, error) {
	switch g := glob.(type) {
	case *naming.GlobReplyEntry:
		service := discovery.Service{
			InstanceId: g.Value.Name,
		}
		for _, server := range g.Value.Servers {
			service.Addrs = append(service.Addrs, server.Server)
		}
		return &service, nil
	case *naming.GlobReplyError:
		return nil, fmt.Errorf("glob error on %s: %v", g.Value.Name, g.Value.Error)
	default:
		return nil, fmt.Errorf("unexpected glob reply %v", g)
	}
}

func mergeAdvertisements(ctx *context.T, prevFound, found map[string]*discovery.Service, updateCh chan<- discovery.Update) {
	for name, service := range found {
		prev := prevFound[name]
		if prev == nil {
			update := discovery.UpdateFound{discovery.Found{Service: copyService(service)}}
			select {
			case updateCh <- update:
			case <-ctx.Done():
				return
			}
			continue
		}

		if !reflect.DeepEqual(prev, service) {
			updates := []discovery.Update{discovery.UpdateLost{discovery.Lost{Service: *prev}}, discovery.UpdateFound{discovery.Found{Service: copyService(service)}}}
			for _, update := range updates {
				select {
				case updateCh <- update:
				case <-ctx.Done():
					return
				}
			}
		}

		delete(prevFound, name)
	}

	for _, prev := range prevFound {
		update := discovery.UpdateLost{discovery.Lost{Service: *prev}}
		select {
		case updateCh <- update:
		case <-ctx.Done():
			return
		}
	}
}
