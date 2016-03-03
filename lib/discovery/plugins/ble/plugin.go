// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// For now, this plugin only works on Linux machines.
// TODO(bjornick): Make this work on Mac and Android.

package ble

import (
	"github.com/pborman/uuid"

	"v.io/v23/context"

	idiscovery "v.io/x/ref/lib/discovery"
)

type blePlugin struct {
	b       *bleNeighborhood
	trigger *idiscovery.Trigger
}

func (b *blePlugin) Advertise(ctx *context.T, adinfo *idiscovery.AdInfo, done func()) error {
	b.b.addAdvertisement(adinfo)
	stop := func() {
		b.b.removeAdvertisement(adinfo)
		done()
	}
	b.trigger.Add(stop, ctx.Done())
	return nil
}

func (b *blePlugin) Scan(ctx *context.T, interfaceName string, scan chan<- *idiscovery.AdInfo, done func()) error {
	serviceUuid := uuid.UUID(idiscovery.NewServiceUUID(interfaceName))
	scanCh, id := b.b.addScanner(serviceUuid)
	drain := func() {
		for range scanCh {
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
			case adinfo := <-scanCh:
				scan <- adinfo
			}
		}
	}()
	return nil
}

func New(_ *context.T, name string) (idiscovery.Plugin, error) {
	b, err := newBleNeighborhood(name)
	if err != nil {
		return nil, err
	}
	return &blePlugin{b: b, trigger: idiscovery.NewTrigger()}, nil
}
