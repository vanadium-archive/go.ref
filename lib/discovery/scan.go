// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
)

func (d *idiscovery) scan(ctx *context.T, session sessionId, query string) (<-chan discovery.Update, error) {
	// TODO(jhahn): Consider to use multiple target services so that the plugins
	// can filter advertisements more efficiently if possible.
	matcher, err := NewMatcher(ctx, query)
	if err != nil {
		return nil, err
	}

	ctx, cancel, err := d.addTask(ctx)
	if err != nil {
		return nil, err
	}

	// TODO(jhahn): Revisit the buffer size.
	scanCh := make(chan *AdInfo, 10)
	barrier := NewBarrier(func() {
		close(scanCh)
		d.removeTask(ctx)
	})
	for _, plugin := range d.plugins {
		if err := plugin.Scan(ctx, matcher.TargetInterfaceName(), scanCh, barrier.Add()); err != nil {
			cancel()
			return nil, err
		}
	}
	// TODO(jhahn): Revisit the buffer size.
	updateCh := make(chan discovery.Update, 10)
	go d.doScan(ctx, session, matcher, scanCh, updateCh)
	return updateCh, nil
}

func (d *idiscovery) doScan(ctx *context.T, session sessionId, matcher Matcher, scanCh <-chan *AdInfo, updateCh chan<- discovery.Update) {
	defer close(updateCh)

	// Some plugins may not return a full advertisement information when it is lost.
	// So we keep the advertisements that we've seen so that we can provide the
	// full advertisement information when it is lost. Note that plugins will not
	// include attachments unless they're tiny enough.
	seen := make(map[discovery.AdId]*AdInfo)
	for {
		select {
		case adinfo, ok := <-scanCh:
			if !ok {
				return
			}
			if err := decrypt(ctx, adinfo); err != nil {
				// Couldn't decrypt it. Ignore it.
				if err != errNoPermission {
					ctx.Error(err)
				}
				continue
			}
			// Filter out advertisements from the same session.
			if d.getAdSession(adinfo) == session {
				continue
			}
			// Note that 'Lost' advertisement may not have full service information.
			// Thus we do not match the query against it. newUpdates() will ignore it
			// if it has not been scanned.
			if !adinfo.Lost {
				matched, err := matcher.Match(&adinfo.Ad)
				if err != nil {
					ctx.Error(err)
					continue
				}
				if !matched {
					continue
				}
			}
			for _, update := range newUpdates(seen, adinfo) {
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

func (d *idiscovery) getAdSession(adinfo *AdInfo) sessionId {
	d.mu.Lock()
	session := d.ads[adinfo.Ad.Id]
	d.mu.Unlock()
	return session
}

func newUpdates(seen map[discovery.AdId]*AdInfo, adinfo *AdInfo) []discovery.Update {
	var updates []discovery.Update
	// The multiple plugins may return the same advertisements. We ignores the update
	// if it has been already sent through the update channel.
	prev := seen[adinfo.Ad.Id]
	if adinfo.Lost {
		// TODO(jhahn): If some plugins return 'Lost' events for an advertisement update, we may
		// generates multiple 'Lost' and 'Found' events for the same update. In order to minimize
		// this flakiness, we may need to delay the handling of 'Lost'.
		if prev != nil {
			delete(seen, prev.Ad.Id)
			prev.Lost = true
			updates = []discovery.Update{NewUpdate(prev)}
		}
	} else {
		// TODO(jhahn): Need to compare the proximity as well.
		switch {
		case prev == nil:
			updates = []discovery.Update{NewUpdate(adinfo)}
			seen[adinfo.Ad.Id] = adinfo
		case prev.Hash != adinfo.Hash:
			prev.Lost = true
			updates = []discovery.Update{NewUpdate(prev), NewUpdate(adinfo)}
			seen[adinfo.Ad.Id] = adinfo
		}
	}
	return updates
}
