// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// For now, this plugin only works on Linux machines.
// TODO(bjornick): Make this work on Mac and Android.

package ble

import (
	"v.io/v23/context"
	"v.io/x/ref/lib/discovery"
)

type blePlugin struct {
	b       *bleNeighborhood
	trigger *discovery.Trigger
}

func (b *blePlugin) Advertise(ctx *context.T, ad discovery.Advertisement, done func()) error {
	b.b.addAdvertisement(newAdvertisment(ad))
	stop := func() {
		b.b.removeService(ad.Service.InstanceUuid)
		done()
	}
	b.trigger.Add(stop, ctx.Done())
	return nil
}

func (b *blePlugin) Scan(ctx *context.T, serviceUuid discovery.Uuid, scan chan<- discovery.Advertisement, done func()) error {
	ch, id := b.b.addScanner(serviceUuid)
	drain := func() {
		for range ch {
		}
	}
	go func() {
		defer func() {
			b.b.removeScanner(id)
			go drain()
			done()
		}()
	L:
		for {
			select {
			case <-ctx.Done():
				break L
			case a := <-ch:
				scan <- *a
			}
		}
	}()
	return nil
}

func NewPlugin(name string) (discovery.Plugin, error) {
	b, err := newBleNeighborhood(name)
	if err != nil {
		return nil, err
	}
	return &blePlugin{b: b, trigger: discovery.NewTrigger()}, nil
}
