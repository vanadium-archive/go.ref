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

	updateCh := make(chan discovery.Update, 10)
	go func() {
		defer close(updateCh)

		var prevFound map[discovery.AdId]*discovery.Advertisement
		for {
			found, err := d.doScan(ctx, matcher.TargetKey(), matcher)
			if found == nil {
				if err != nil {
					ctx.Error(err)
				}
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
	// If the target is neither empty nor a valid AdId, we return without an error,
	// since there will be not entries with the requested target length in the namespace.
	if len(target) > 0 {
		if _, err := discovery.ParseAdId(target); err != nil {
			return nil, nil
		}
	}

	// Now that the key is either empty or an AdId, we suffix it with "*".
	// In the case of empty, we need to scan for everything.
	// In the case where target is a AdId we need to scan for entries prefixed with
	// the AdId with any encoded attributes afterwards.
	scanCh, err := d.ns.Glob(ctx, target+"*")
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
			// Since mount operations are not atomic, we may not have addresses yet.
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
		ad, err := decodeAdFromSuffix(g.Value.Name)
		if err != nil {
			return nil, err
		}
		addrs := make([]string, 0, len(g.Value.Servers))
		for _, server := range g.Value.Servers {
			addrs = append(addrs, server.Server)
		}
		// We sort the addresses to avoid false updates.
		sort.Strings(addrs)
		ad.Addresses = addrs
		return ad, nil
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
