// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"reflect"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
)

// Scan implements discovery.Scanner.
func (ds *ds) Scan(ctx *context.T, query string) (<-chan discovery.Update, error) {
	// TODO(jhahn): Consider to use multiple target services so that the plugins
	// can filter advertisements more efficiently if possible.
	matcher, targetInterfaceName, err := newMatcher(ctx, query)
	if err != nil {
		return nil, err
	}
	var targetServiceUuid Uuid
	if len(targetInterfaceName) > 0 {
		targetServiceUuid = NewServiceUUID(targetInterfaceName)
	}

	ctx, cancel, err := ds.addTask(ctx)
	if err != nil {
		return nil, err
	}

	// TODO(jhahn): Revisit the buffer size.
	scanCh := make(chan Advertisement, 10)
	barrier := NewBarrier(func() {
		close(scanCh)
		ds.removeTask(ctx)
	})
	for _, plugin := range ds.plugins {
		if err := plugin.Scan(ctx, targetServiceUuid, scanCh, barrier.Add()); err != nil {
			cancel()
			return nil, err
		}
	}
	// TODO(jhahn): Revisit the buffer size.
	updateCh := make(chan discovery.Update, 10)
	go doScan(ctx, matcher, scanCh, updateCh)
	return updateCh, nil
}

func doScan(ctx *context.T, matcher matcher, scanCh <-chan Advertisement, updateCh chan<- discovery.Update) {
	defer close(updateCh)

	// Get the blessing names belong to the principal.
	//
	// TODO(jhahn): It isn't clear that we will always have the blessing required to decrypt
	// the advertisement as their "default" blessing - indeed it may not even be in the store.
	// Revisit this issue.
	principal := v23.GetPrincipal(ctx)
	var names []string
	if principal != nil {
		names = security.BlessingNames(principal, principal.BlessingStore().Default())
	}

	found := make(map[string]*Advertisement)
	for {
		select {
		case ad := <-scanCh:
			if err := decrypt(&ad, names); err != nil {
				// Couldn't decrypt it. Ignore it.
				if err != errNoPermission {
					ctx.Error(err)
				}
				continue
			}
			// Note that 'Lost' advertisement may not have full service information.
			// Thus we do not match the query against it. mergeAdvertisement() will
			// ignore it if it has not been scanned.
			if !ad.Lost {
				if !matcher.match(&ad) {
					continue
				}
			}
			for _, update := range mergeAdvertisement(found, &ad) {
				select {
				case updateCh <- update:
				case <-ctx.Done():
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func mergeAdvertisement(found map[string]*Advertisement, ad *Advertisement) (updates []discovery.Update) {
	// The multiple plugins may return the same advertisements. We ignores the update
	// if it has been already sent through the update channel.
	prev := found[ad.Service.InstanceId]
	if ad.Lost {
		// TODO(jhahn): If some plugins return 'Lost' events for an advertisement update, we may
		// generates multiple 'Lost' and 'Found' events for the same update. In order to minimize
		// this flakiness, we may need to delay the handling of 'Lost'.
		if prev != nil {
			delete(found, ad.Service.InstanceId)
			updates = []discovery.Update{discovery.UpdateLost{discovery.Lost{InstanceId: ad.Service.InstanceId}}}
		}
	} else {
		// TODO(jhahn): Need to compare the proximity as well.
		switch {
		case prev == nil:
			updates = []discovery.Update{discovery.UpdateFound{discovery.Found{Service: ad.Service}}}
		case !reflect.DeepEqual(prev.Service, ad.Service):
			updates = []discovery.Update{
				discovery.UpdateLost{discovery.Lost{InstanceId: ad.Service.InstanceId}},
				discovery.UpdateFound{discovery.Found{Service: copyService(&ad.Service)}},
			}
		}
		found[ad.Service.InstanceId] = ad
	}
	return
}
