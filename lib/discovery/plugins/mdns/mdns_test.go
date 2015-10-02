// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mdns

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pborman/uuid"

	"v.io/v23/context"
	"v.io/v23/discovery"

	ldiscovery "v.io/x/ref/lib/discovery"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test"
)

func encryptionKeys(key []byte) []ldiscovery.EncryptionKey {
	return []ldiscovery.EncryptionKey{ldiscovery.EncryptionKey(fmt.Sprintf("key:%x", key))}
}

func advertise(ctx *context.T, p ldiscovery.Plugin, service discovery.Service) (func(), error) {
	ctx, stop := context.WithCancel(ctx)
	ad := ldiscovery.Advertisement{
		ServiceUuid:         ldiscovery.NewServiceUUID(service.InterfaceName),
		Service:             service,
		EncryptionAlgorithm: ldiscovery.TestEncryption,
		EncryptionKeys:      encryptionKeys(service.InstanceUuid),
	}
	if err := p.Advertise(ctx, ad); err != nil {
		return nil, fmt.Errorf("Advertise failed: %v", err)
	}
	return stop, nil
}

func startScan(ctx *context.T, p ldiscovery.Plugin, interfaceName string) (<-chan ldiscovery.Advertisement, func(), error) {
	ctx, stop := context.WithCancel(ctx)
	scan := make(chan ldiscovery.Advertisement)
	var serviceUuid uuid.UUID
	if len(interfaceName) > 0 {
		serviceUuid = ldiscovery.NewServiceUUID(interfaceName)
	}
	if err := p.Scan(ctx, serviceUuid, scan); err != nil {
		return nil, nil, fmt.Errorf("Scan failed: %v", err)
	}
	return scan, stop, nil
}

func scan(ctx *context.T, p ldiscovery.Plugin, interfaceName string) ([]ldiscovery.Advertisement, error) {
	scan, stop, err := startScan(ctx, p, interfaceName)
	if err != nil {
		return nil, err
	}
	defer stop()

	var ads []ldiscovery.Advertisement
	for {
		select {
		case ad := <-scan:
			ads = append(ads, ad)
		case <-time.After(10 * time.Millisecond):
			return ads, nil
		}
	}
}

func match(ads []ldiscovery.Advertisement, lost bool, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, ad := range ads {
			if !uuid.Equal(ad.InstanceUuid, want.InstanceUuid) {
				continue
			}
			if lost {
				matched = ad.Lost
			} else {
				matched = !ad.Lost && reflect.DeepEqual(ad.Service, want) && ad.EncryptionAlgorithm == ldiscovery.TestEncryption && reflect.DeepEqual(ad.EncryptionKeys, encryptionKeys(want.InstanceUuid))
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

func scanAndMatch(ctx *context.T, p ldiscovery.Plugin, interfaceName string, wants ...discovery.Service) error {
	const timeout = 3 * time.Second

	var ads []ldiscovery.Advertisement
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		ads, err = scan(ctx, p, interfaceName)
		if err != nil {
			return err
		}
		if matchFound(ads, wants...) {
			return nil
		}
	}
	return fmt.Errorf("Match failed; got %v, but wanted %v", ads, wants)
}

func TestBasic(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	services := []discovery.Service{
		{
			InstanceUuid:  ldiscovery.NewInstanceUUID(),
			InstanceName:  "service1",
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
			InstanceName:  "service2",
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
			InstanceName:  "service3",
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

	var stops []func()
	for _, service := range services {
		stop, err := advertise(ctx, p1, service)
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}

	p2, err := newWithLoopback("m2", true)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Make sure all advertisements are discovered.
	if err := scanAndMatch(ctx, p2, "v.io/x", services[0], services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "v.io/y", services[2]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "", services...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "v.io/z"); err != nil {
		t.Error(err)
	}

	// Make sure it is not discovered when advertising is stopped.
	stops[0]()
	if err := scanAndMatch(ctx, p2, "v.io/x", services[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "", services[1], services[2]); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scan, scanStop, err := startScan(ctx, p2, "v.io/y")
	if err != nil {
		t.Error(err)
	}
	defer scanStop()
	ad := <-scan
	if !matchFound([]ldiscovery.Advertisement{ad}, services[2]) {
		t.Errorf("Unexpected scan: %v, but want %v", ad, services[2])
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[2]()

	ad = <-scan
	if !matchLost([]ldiscovery.Advertisement{ad}, services[2]) {
		t.Errorf("Unexpected scan: %v, but want %v as lost", ad, services[2])
	}

	// Stop advertising the remaining one; Shouldn't discover anything.
	stops[1]()
	if err := scanAndMatch(ctx, p2, ""); err != nil {
		t.Error(err)
	}
}

func TestLargeTxt(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	service := discovery.Service{
		InstanceUuid:  ldiscovery.NewInstanceUUID(),
		InstanceName:  "service2",
		InterfaceName: strings.Repeat("i", 280),
		Attrs: discovery.Attributes{
			"k": strings.Repeat("v", 280),
		},
		Addrs: []string{
			strings.Repeat("a1", 100),
			strings.Repeat("a2", 100),
		},
	}

	p1, err := newWithLoopback("m1", true)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	stop, err := advertise(ctx, p1, service)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	p2, err := newWithLoopback("m2", true)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := scanAndMatch(ctx, p2, "", service); err != nil {
		t.Error(err)
	}
}
