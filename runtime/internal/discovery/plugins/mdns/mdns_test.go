// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mdns

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/pborman/uuid"

	"v.io/v23/context"
	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/runtime/internal/discovery"
)

func TestBasic(t *testing.T) {
	services := []discovery.Service{
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/x",
			Attrs: discovery.Attributes{
				"a": "a1234",
				"b": "b1234",
			},
			Addrs: []string{
				"/@6@wsh@foo.com:1234@@/x",
			},
		},
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/x",
			Attrs: discovery.Attributes{
				"a": "a5678",
				"b": "b5678",
			},
			Addrs: []string{
				"/@6@wsh@bar.com:1234@@/x",
			},
		},
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
			InterfaceName: "v.io/y",
			Attrs: discovery.Attributes{
				"c": "c1234",
				"d": "d1234",
			},
			Addrs: []string{
				"/@6@wsh@foo.com:1234@@/y",
				"/@6@wsh@bar.com:1234@@/y",
			},
		},
	}

	p1, err := newWithLoopback("m1", true)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	p2, err := newWithLoopback("m2", true)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	var stops []func()
	for _, service := range services {
		stop, err := advertise(p1, service)
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}

	if err := scanAndMatch(p2, "v.io/x", services[0], services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(p2, "v.io/y", services[2]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(p2, "", services...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(p2, "v.io/z"); err != nil {
		t.Error(err)
	}

	stops[0]()
	if err := scanAndMatch(p2, "v.io/x", services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(p2, "", services[1], services[2]); err != nil {
		t.Error(err)
	}
	stops[1]()
	stops[2]()
	if err := scanAndMatch(p2, ""); err != nil {
		t.Error(err)
	}
}

func advertise(p idiscovery.Plugin, service discovery.Service) (func(), error) {
	ctx, cancel := context.RootContext()
	ad := idiscovery.Advertisement{
		ServiceUuid: idiscovery.NewServiceUUID(service.InterfaceName),
		Service:     service,
	}
	if err := p.Advertise(ctx, &ad); err != nil {
		return nil, fmt.Errorf("Advertise failed: %v", err)
	}
	return cancel, nil
}

func scan(p idiscovery.Plugin, interfaceName string) ([]idiscovery.Advertisement, error) {
	ctx, _ := context.RootContext()
	scanCh := make(chan *idiscovery.Advertisement)
	var serviceUuid uuid.UUID
	if len(interfaceName) > 0 {
		serviceUuid = idiscovery.NewServiceUUID(interfaceName)
	}
	if err := p.Scan(ctx, serviceUuid, scanCh); err != nil {
		return nil, fmt.Errorf("Scan failed: %v", err)
	}
	var ads []idiscovery.Advertisement
	for {
		select {
		case ad := <-scanCh:
			ads = append(ads, *ad)
		case <-time.After(10 * time.Millisecond):
			return ads, nil
		}
	}
}

func match(ads []idiscovery.Advertisement, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, ad := range ads {
			if !uuid.Equal(ad.ServiceUuid, idiscovery.NewServiceUUID(want.InterfaceName)) {
				continue
			}
			matched = reflect.DeepEqual(ad.Service, want)
			if matched {
				ads = append(ads[:i], ads[i+1:]...)
				break
			}
		}
		if !matched {
			return false
		}
	}
	return len(ads) == 0
}

func scanAndMatch(p idiscovery.Plugin, interfaceName string, wants ...discovery.Service) error {
	const timeout = 1 * time.Second

	var ads []idiscovery.Advertisement
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		ads, err = scan(p, interfaceName)
		if err != nil {
			return err
		}
		if match(ads, wants...) {
			return nil
		}
	}
	return fmt.Errorf("Match failed; got %v, but wanted %v", ads, wants)
}
