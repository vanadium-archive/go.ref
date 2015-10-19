// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"reflect"
	"sync"

	"github.com/pborman/uuid"

	"v.io/v23/context"

	"v.io/x/ref/lib/discovery"
)

type plugin struct {
	mu       sync.Mutex
	services map[string][]discovery.Advertisement // GUARDED_BY(mu)

	updated   *sync.Cond
	updateSeq int
}

func (p *plugin) Advertise(ctx *context.T, ad discovery.Advertisement, done func()) error {
	p.mu.Lock()
	key := string(ad.ServiceUuid)
	ads := p.services[key]
	p.services[key] = append(ads, ad)
	p.updateSeq++
	p.mu.Unlock()
	p.updated.Broadcast()

	go func() {
		defer done()
		<-ctx.Done()

		p.mu.Lock()
		ads := p.services[key]
		for i, a := range ads {
			if uuid.Equal(uuid.UUID(a.Service.InstanceUuid), uuid.UUID(ad.Service.InstanceUuid)) {
				ads = append(ads[:i], ads[i+1:]...)
				break
			}
		}
		if len(ads) > 0 {
			p.services[key] = ads
		} else {
			delete(p.services, key)
		}
		p.updateSeq++
		p.mu.Unlock()
		p.updated.Broadcast()
	}()
	return nil
}

func (p *plugin) Scan(ctx *context.T, serviceUuid discovery.Uuid, ch chan<- discovery.Advertisement, done func()) error {
	rescan := make(chan struct{})
	go func() {
		var updateSeqSeen int
		for {
			p.updated.L.Lock()
			for updateSeqSeen == p.updateSeq {
				p.updated.Wait()
			}
			updateSeqSeen = p.updateSeq
			p.updated.L.Unlock()
			select {
			case rescan <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer done()

		scanned := make(map[string]discovery.Advertisement)
		for {
			current := make(map[string]discovery.Advertisement)
			p.mu.Lock()
			for key, ads := range p.services {
				if len(serviceUuid) > 0 && key != string(serviceUuid) {
					continue
				}
				for _, ad := range ads {
					current[string(ad.Service.InstanceUuid)] = ad
				}
			}
			p.mu.Unlock()

			changed := make([]discovery.Advertisement, 0, len(current))
			for key, ad := range current {
				old, ok := scanned[key]
				if !ok || !reflect.DeepEqual(old, ad) {
					changed = append(changed, ad)
				}
			}
			for key, ad := range scanned {
				if _, ok := current[key]; !ok {
					ad.Lost = true
					changed = append(changed, ad)
				}
			}

			// Push new changes.
			for _, ad := range changed {
				select {
				case ch <- ad:
				case <-ctx.Done():
					return
				}
			}

			scanned = current

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

func New() discovery.Plugin {
	return &plugin{
		services: make(map[string][]discovery.Advertisement),
		updated:  sync.NewCond(&sync.Mutex{}),
	}
}
