// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"reflect"
	"sync"

	"v.io/v23/context"
	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
)

type plugin struct {
	mu sync.Mutex

	updated   *sync.Cond
	updateSeq int // GUARDED_BY(mu)

	adinfoMap map[string]map[discovery.AdId]*idiscovery.AdInfo // GUARDED_BY(mu)
}

func (p *plugin) Advertise(ctx *context.T, adinfo *idiscovery.AdInfo, done func()) error {
	p.RegisterAd(adinfo)
	go func() {
		<-ctx.Done()
		p.UnregisterAd(adinfo)
		done()
	}()
	return nil
}

func (p *plugin) Scan(ctx *context.T, interfaceName string, ch chan<- *idiscovery.AdInfo, done func()) error {
	rescan := make(chan struct{})
	go func() {
		defer close(rescan)

		var updateSeqSeen int
		for {
			p.mu.Lock()
			for updateSeqSeen == p.updateSeq {
				p.updated.Wait()
			}
			updateSeqSeen = p.updateSeq
			p.mu.Unlock()
			select {
			case rescan <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer done()

		seen := make(map[discovery.AdId]idiscovery.AdInfo)
		for {
			current := make(map[discovery.AdId]idiscovery.AdInfo)
			p.mu.Lock()
			for key, adinfos := range p.adinfoMap {
				if len(interfaceName) > 0 && key != interfaceName {
					continue
				}
				for id, adinfo := range adinfos {
					current[id] = *adinfo
				}
			}
			p.mu.Unlock()

			changed := make([]idiscovery.AdInfo, 0, len(current))
			for id, adinfo := range current {
				old, ok := seen[id]
				if !ok || !reflect.DeepEqual(old, adinfo) {
					changed = append(changed, adinfo)
				}
			}
			for id, adinfo := range seen {
				if _, ok := current[id]; !ok {
					adinfo.Lost = true
					changed = append(changed, adinfo)
				}
			}

			// Push new changes.
			for i := range changed {
				select {
				case ch <- &changed[i]:
				case <-ctx.Done():
					return
				}
			}

			seen = current

			// Wait the next update.
			select {
			case <-rescan:
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

// RegisterAd registers an advertisement to the plugin. If there is already an
// advertisement with the same id, it will be updated with the given advertisement.
func (p *plugin) RegisterAd(adinfo *idiscovery.AdInfo) {
	p.mu.Lock()
	adinfos := p.adinfoMap[adinfo.Ad.InterfaceName]
	if adinfos == nil {
		adinfos = make(map[discovery.AdId]*idiscovery.AdInfo)
		p.adinfoMap[adinfo.Ad.InterfaceName] = adinfos
	}
	adinfos[adinfo.Ad.Id] = adinfo
	p.updateSeq++
	p.mu.Unlock()
	p.updated.Broadcast()
}

// UnregisterAd unregisters a registered advertisement from the plugin.
func (p *plugin) UnregisterAd(adinfo *idiscovery.AdInfo) {
	p.mu.Lock()
	adinfos := p.adinfoMap[adinfo.Ad.InterfaceName]
	delete(adinfos, adinfo.Ad.Id)
	if len(adinfos) == 0 {
		p.adinfoMap[adinfo.Ad.InterfaceName] = nil
	}
	p.updateSeq++
	p.mu.Unlock()
	p.updated.Broadcast()
}

func New() *plugin {
	p := &plugin{adinfoMap: make(map[string]map[discovery.AdId]*idiscovery.AdInfo)}
	p.updated = sync.NewCond(&p.mu)
	return p
}
