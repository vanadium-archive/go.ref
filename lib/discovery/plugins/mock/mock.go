// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"sync"

	"github.com/pborman/uuid"

	"v.io/v23/context"

	"v.io/x/ref/lib/discovery"
)

type plugin struct {
	mu       sync.Mutex
	services map[string][]*discovery.Advertisement // GUARDED_BY(mu)
}

func (p *plugin) Advertise(ctx *context.T, ad *discovery.Advertisement) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := string(ad.ServiceUuid)
	ads := p.services[key]
	p.services[key] = append(ads, ad)
	go func() {
		<-ctx.Done()
		p.mu.Lock()
		defer p.mu.Unlock()
		ads := p.services[key]
		for i, a := range ads {
			if uuid.Equal(a.InstanceUuid, ad.InstanceUuid) {
				ads = append(ads[:i], ads[i+1:]...)
				break
			}
		}
		if len(ads) > 0 {
			p.services[key] = ads
		} else {
			delete(p.services, key)
		}
	}()
	return nil
}

func (p *plugin) Scan(ctx *context.T, serviceUuid uuid.UUID, scanCh chan<- *discovery.Advertisement) error {
	go func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for key, service := range p.services {
			if len(serviceUuid) > 0 && key != string(serviceUuid) {
				continue
			}
			for _, ad := range service {
				select {
				case scanCh <- ad:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return nil
}

func New() discovery.Plugin {
	return &plugin{services: make(map[string][]*discovery.Advertisement)}
}
