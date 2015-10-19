// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mdns

import (
	"fmt"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pborman/uuid"

	"v.io/v23/context"
	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/test"
)

var (
	testPort          int
	unusedTCPListener *net.TCPListener
)

func init() {
	// Test with an unused UDP port to avoid interference from others.
	//
	// We try to find an available TCP port since we cannot open multicast UDP
	// connection with an opened UDP port.
	unusedTCPListener, _ = net.ListenTCP("tcp", &net.TCPAddr{})
	_, port, _ := net.SplitHostPort(unusedTCPListener.Addr().String())
	testPort, _ = strconv.Atoi(port)
}

func newMDNS(host string) (idiscovery.Plugin, error) {
	return newWithLoopback(host, testPort, true)
}

func encryptionKeys(key []byte) []idiscovery.EncryptionKey {
	return []idiscovery.EncryptionKey{idiscovery.EncryptionKey(fmt.Sprintf("key:%x", key))}
}

func advertise(ctx *context.T, p idiscovery.Plugin, service discovery.Service) (func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	ad := idiscovery.Advertisement{
		ServiceUuid:         idiscovery.NewServiceUUID(service.InterfaceName),
		Service:             service,
		EncryptionAlgorithm: idiscovery.TestEncryption,
		EncryptionKeys:      encryptionKeys(service.InstanceUuid),
	}
	var wg sync.WaitGroup
	wg.Add(1)
	if err := p.Advertise(ctx, ad, wg.Done); err != nil {
		return nil, fmt.Errorf("Advertise failed: %v", err)
	}
	stop := func() {
		cancel()
		wg.Wait()
	}
	return stop, nil
}

func startScan(ctx *context.T, p idiscovery.Plugin, interfaceName string) (<-chan idiscovery.Advertisement, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	scan := make(chan idiscovery.Advertisement)
	var serviceUuid idiscovery.Uuid
	if len(interfaceName) > 0 {
		serviceUuid = idiscovery.NewServiceUUID(interfaceName)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	if err := p.Scan(ctx, serviceUuid, scan, wg.Done); err != nil {
		return nil, nil, fmt.Errorf("Scan failed: %v", err)
	}
	stop := func() {
		cancel()
		wg.Wait()
	}
	return scan, stop, nil
}

func scan(ctx *context.T, p idiscovery.Plugin, interfaceName string) ([]idiscovery.Advertisement, error) {
	scan, stop, err := startScan(ctx, p, interfaceName)
	if err != nil {
		return nil, err
	}
	defer stop()

	var ads []idiscovery.Advertisement
	for {
		select {
		case ad := <-scan:
			ads = append(ads, ad)
		case <-time.After(10 * time.Millisecond):
			return ads, nil
		}
	}
}

func match(ads []idiscovery.Advertisement, lost bool, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, ad := range ads {
			if !uuid.Equal(ad.Service.InstanceUuid, want.InstanceUuid) {
				continue
			}
			if lost {
				matched = ad.Lost
			} else {
				matched = !ad.Lost && reflect.DeepEqual(ad.Service, want) && ad.EncryptionAlgorithm == idiscovery.TestEncryption && reflect.DeepEqual(ad.EncryptionKeys, encryptionKeys(want.InstanceUuid))
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

func matchFound(ads []idiscovery.Advertisement, wants ...discovery.Service) bool {
	return match(ads, false, wants...)
}

func matchLost(ads []idiscovery.Advertisement, wants ...discovery.Service) bool {
	return match(ads, true, wants...)
}

func scanAndMatch(ctx *context.T, p idiscovery.Plugin, interfaceName string, wants ...discovery.Service) error {
	const timeout = 3 * time.Second

	var ads []idiscovery.Advertisement
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
	ctx, shutdown := test.TestContext()
	defer shutdown()

	services := []discovery.Service{
		{
			InstanceUuid:  idiscovery.NewInstanceUUID(),
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
			InstanceUuid:  idiscovery.NewInstanceUUID(),
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
			InstanceUuid:  idiscovery.NewInstanceUUID(),
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

	p1, err := newMDNS("m1")
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

	p2, err := newMDNS("m2")
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
	if !matchFound([]idiscovery.Advertisement{ad}, services[2]) {
		t.Errorf("Unexpected scan: %v, but want %v", ad, services[2])
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[2]()

	ad = <-scan
	if !matchLost([]idiscovery.Advertisement{ad}, services[2]) {
		t.Errorf("Unexpected scan: %v, but want %v as lost", ad, services[2])
	}

	// Stop advertising the remaining one; Shouldn't discover anything.
	stops[1]()
	if err := scanAndMatch(ctx, p2, ""); err != nil {
		t.Error(err)
	}
}

func TestLargeTxt(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	service := discovery.Service{
		InstanceUuid:  idiscovery.NewInstanceUUID(),
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

	p1, err := newMDNS("m1")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	stop, err := advertise(ctx, p1, service)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	p2, err := newMDNS("m2")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := scanAndMatch(ctx, p2, "", service); err != nil {
		t.Error(err)
	}
}
