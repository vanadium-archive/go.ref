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

	ldiscovery "v.io/x/ref/lib/discovery"
)

func TestBasic(t *testing.T) {
	services := []discovery.Service{
		{
			InstanceUuid:  ldiscovery.NewInstanceUUID(),
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
			InstanceUuid:  ldiscovery.NewInstanceUUID(),
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
			InstanceUuid:  ldiscovery.NewInstanceUUID(),
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

	// Make sure all advertisements are discovered.
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

	// Make sure it is not discovered when advertising is stopped.
	stops[0]()
	if err := scanAndMatch(p2, "v.io/x", services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(p2, "", services[1], services[2]); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scan, scanStop, err := startScan(p2, "v.io/y")
	if err != nil {
		t.Error(err)
	}
	defer scanStop()
	ad := *<-scan
	if !matchFound([]ldiscovery.Advertisement{ad}, services[2]) {
		t.Errorf("Unexpected scan: %v", ad)
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[2]()

	ad = *<-scan
	if !matchLost([]ldiscovery.Advertisement{ad}, services[2]) {
		t.Errorf("Unexpected scan: %v", ad)
	}

	// Stop advertising the remaining one; Shouldn't discover anything.
	stops[1]()
	if err := scanAndMatch(p2, ""); err != nil {
		t.Error(err)
	}
}

func advertise(p ldiscovery.Plugin, service discovery.Service) (func(), error) {
	ctx, cancel := context.RootContext()
	ad := ldiscovery.Advertisement{
		ServiceUuid: ldiscovery.NewServiceUUID(service.InterfaceName),
		Service:     service,
	}
	if err := p.Advertise(ctx, &ad); err != nil {
		return nil, fmt.Errorf("Advertise failed: %v", err)
	}
	return cancel, nil
}

func startScan(p ldiscovery.Plugin, interfaceName string) (<-chan *ldiscovery.Advertisement, func(), error) {
	ctx, stop := context.RootContext()
	scan := make(chan *ldiscovery.Advertisement)
	var serviceUuid uuid.UUID
	if len(interfaceName) > 0 {
		serviceUuid = ldiscovery.NewServiceUUID(interfaceName)
	}
	if err := p.Scan(ctx, serviceUuid, scan); err != nil {
		return nil, nil, fmt.Errorf("Scan failed: %v", err)
	}
	return scan, stop, nil
}

func scan(p ldiscovery.Plugin, interfaceName string) ([]ldiscovery.Advertisement, error) {
	scan, stop, err := startScan(p, interfaceName)
	if err != nil {
		return nil, err
	}
	defer stop()

	var ads []ldiscovery.Advertisement
	for {
		select {
		case ad := <-scan:
			ads = append(ads, *ad)
		case <-time.After(10 * time.Millisecond):
			return ads, nil
		}
	}
}

func match(ads []ldiscovery.Advertisement, lost bool, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, ad := range ads {
			if !uuid.Equal(ad.ServiceUuid, ldiscovery.NewServiceUUID(want.InterfaceName)) {
				continue
			}
			if lost {
				matched = ad.Lost
			} else {
				matched = lost || reflect.DeepEqual(ad.Service, want)
			}
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

func matchFound(ads []ldiscovery.Advertisement, wants ...discovery.Service) bool {
	return match(ads, false, wants...)
}

func matchLost(ads []ldiscovery.Advertisement, wants ...discovery.Service) bool {
	return match(ads, true, wants...)
}

func scanAndMatch(p ldiscovery.Plugin, interfaceName string, wants ...discovery.Service) error {
	const timeout = 1 * time.Second

	var ads []ldiscovery.Advertisement
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		ads, err = scan(p, interfaceName)
		if err != nil {
			return err
		}
		if matchFound(ads, wants...) {
			return nil
		}
	}
	return fmt.Errorf("Match failed; got %v, but wanted %v", ads, wants)
}
