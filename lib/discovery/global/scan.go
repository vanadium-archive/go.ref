// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"fmt"
	"reflect"
	"sort"

	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/naming"

	idiscovery "v.io/x/ref/lib/discovery"
)

func (d *gdiscovery) Scan(ctx *context.T, query string) (<-chan discovery.Update, error) {
	matcher, err := idiscovery.NewMatcher(ctx, query)
	if err != nil {
		return nil, err
	}
	target := matcher.TargetKey()
	if len(target) == 0 {
		target = "*"
	}

	updateCh := make(chan discovery.Update, 10)
	go func() {
		defer close(updateCh)

		var prevFound map[discovery.AdId]*discovery.Advertisement
		for {
			found, err := d.doScan(ctx, target, matcher)
			if err != nil {
				ctx.Error(err)
			} else {
				sendUpdates(ctx, prevFound, found, updateCh)
				prevFound = found
			}

			select {
			case <-d.clock.After(d.scanInterval):
			case <-ctx.Done():
				return
			}
		}
	}()
	return updateCh, nil
}

func (d *gdiscovery) doScan(ctx *context.T, target string, matcher idiscovery.Matcher) (map[discovery.AdId]*discovery.Advertisement, error) {
	scanCh, err := d.ns.Glob(ctx, target)
	if err != nil {
		return nil, err
	}
	defer func() {
		for range scanCh {
		}
	}()

	found := make(map[discovery.AdId]*discovery.Advertisement)
	for {
		select {
		case glob, ok := <-scanCh:
			if !ok {
				return found, nil
			}
			ad, err := convToAd(glob)
			if err != nil {
				ctx.Error(err)
				continue
			}
			// Since mount operation is not atomic, we may not have addresses yet.
			// Ignore it. It will be re-scanned in the next cycle.
			if len(ad.Addresses) == 0 {
				continue
			}
			// Filter out advertisements from the same discovery instance.
			if d.hasAd(ad) {
				continue
			}
			matched, err := matcher.Match(ad)
			if err != nil {
				ctx.Error(err)
				continue
			}
			if !matched {
				continue
			}
			found[ad.Id] = ad
		case <-ctx.Done():
			return nil, nil
		}
	}
}

func (d *gdiscovery) hasAd(ad *discovery.Advertisement) bool {
	d.mu.Lock()
	_, ok := d.ads[ad.Id]
	d.mu.Unlock()
	return ok
}

func convToAd(glob naming.GlobReply) (*discovery.Advertisement, error) {
	switch g := glob.(type) {
	case *naming.GlobReplyEntry:
		id, err := discovery.ParseAdId(g.Value.Name)
		if err != nil {
			return nil, err
		}
		addrs := make([]string, 0, len(g.Value.Servers))
		for _, server := range g.Value.Servers {
			addrs = append(addrs, server.Server)
		}
		// We sort the addresses to avoid false update.
		sort.Strings(addrs)
		return &discovery.Advertisement{Id: id, Addresses: addrs}, nil
	case *naming.GlobReplyError:
		return nil, fmt.Errorf("glob error on %s: %v", g.Value.Name, g.Value.Error)
	default:
		return nil, fmt.Errorf("unexpected glob reply %v", g)
	}
}

func sendUpdates(ctx *context.T, prevFound, found map[discovery.AdId]*discovery.Advertisement, updateCh chan<- discovery.Update) {
	for id, ad := range found {
		var updates []discovery.Update
		if prev := prevFound[id]; prev == nil {
			updates = []discovery.Update{idiscovery.NewUpdate(&idiscovery.AdInfo{Ad: *ad})}
		} else {
			if !reflect.DeepEqual(prev, ad) {
				updates = []discovery.Update{
					idiscovery.NewUpdate(&idiscovery.AdInfo{Ad: *prev, Lost: true}),
					idiscovery.NewUpdate(&idiscovery.AdInfo{Ad: *ad}),
				}
			}
			delete(prevFound, id)
		}
		for _, update := range updates {
			select {
			case updateCh <- update:
			case <-ctx.Done():
				return
			}
		}
	}

	for _, prev := range prevFound {
		update := idiscovery.NewUpdate(&idiscovery.AdInfo{Ad: *prev, Lost: true})
		select {
		case updateCh <- update:
		case <-ctx.Done():
			return
		}
	}
}
