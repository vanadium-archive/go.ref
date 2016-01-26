// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mdns

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

func encryptionKeys(key string) []idiscovery.EncryptionKey {
	return []idiscovery.EncryptionKey{idiscovery.EncryptionKey(fmt.Sprintf("key:%x", key))}
}

func advertise(ctx *context.T, p idiscovery.Plugin, ad idiscovery.Advertisement) (func(), error) {
	ctx, cancel := context.WithCancel(ctx)
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
	var wg sync.WaitGroup
	wg.Add(1)
	if err := p.Scan(ctx, interfaceName, scan, wg.Done); err != nil {
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

func match(ads []idiscovery.Advertisement, lost bool, wants ...idiscovery.Advertisement) bool {
	for _, want := range wants {
		matched := false
		for i, ad := range ads {
			if ad.Service.InstanceId != want.Service.InstanceId {
				continue
			}
			if lost {
				matched = ad.Lost
			} else {
				matched = reflect.DeepEqual(ad, want)
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

func matchFound(ads []idiscovery.Advertisement, wants ...idiscovery.Advertisement) bool {
	return match(ads, false, wants...)
}

func matchLost(ads []idiscovery.Advertisement, wants ...idiscovery.Advertisement) bool {
	return match(ads, true, wants...)
}

func scanAndMatch(ctx *context.T, p idiscovery.Plugin, interfaceName string, wants ...idiscovery.Advertisement) error {
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
	return fmt.Errorf("Match failed; got %#v, but wanted %#v", ads, wants)
}

func TestBasic(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	ads := []idiscovery.Advertisement{
		{
			Service: discovery.Service{
				InstanceId:    "123",
				InstanceName:  "service1",
				InterfaceName: "v.io/x",
				Attrs: discovery.Attributes{
					"a": "a1234",
					"b": "b1234",
				},
				Addrs: []string{
					"/@6@wsh@foo.com:1234@@/x",
				},
				Attachments: discovery.Attachments{
					"a": []byte{11, 12, 13},
					"p": []byte{21, 22, 23},
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("123"),
			Hash:                []byte{1, 2, 3},
			DirAddrs: []string{
				"/@6@wsh@foo.com:1234@@/d",
			},
		},
		{
			Service: discovery.Service{
				InstanceId:    "456",
				InstanceName:  "service2",
				InterfaceName: "v.io/x",
				Attrs: discovery.Attributes{
					"a": "a5678",
					"b": "b5678",
				},
				Addrs: []string{
					"/@6@wsh@bar.com:1234@@/x",
				},
				Attachments: discovery.Attachments{
					"a": []byte{31, 32, 33},
					"p": []byte{41, 42, 43},
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("456"),
			Hash:                []byte{4, 5, 6},
			DirAddrs: []string{
				"/@6@wsh@bar.com:1234@@/d",
			},
		},
		{
			Service: discovery.Service{
				InstanceId:    "789",
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
				Attachments: discovery.Attachments{
					"c": []byte{51, 52, 53},
					"p": []byte{61, 62, 63},
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("789"),
			Hash:                []byte{7, 8, 9},
			DirAddrs: []string{
				"/@6@wsh@foo.com:1234@@/d",
				"/@6@wsh@bar.com:1234@@/d",
			},
		},
	}

	p1, err := newMDNS("m1")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	var stops []func()
	for _, ad := range ads {
		stop, err := advertise(ctx, p1, ad)
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
	if err := scanAndMatch(ctx, p2, "v.io/x", ads[0], ads[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "v.io/y", ads[2]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "", ads...); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "v.io/z"); err != nil {
		t.Error(err)
	}

	// Make sure it is not discovered when advertising is stopped.
	stops[0]()
	if err := scanAndMatch(ctx, p2, "v.io/x", ads[1]); err != nil {
		t.Error(err)
	}
	if err := scanAndMatch(ctx, p2, "", ads[1], ads[2]); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scan, scanStop, err := startScan(ctx, p2, "v.io/y")
	if err != nil {
		t.Error(err)
	}
	defer scanStop()
	ad := <-scan
	if !matchFound([]idiscovery.Advertisement{ad}, ads[2]) {
		t.Errorf("Unexpected scan: %v, but want %v", ad, ads[2])
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[2]()

	ad = <-scan
	if !matchLost([]idiscovery.Advertisement{ad}, ads[2]) {
		t.Errorf("Unexpected scan: %v, but want %v as lost", ad, ads[2])
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

	ads := []idiscovery.Advertisement{
		{
			Service: discovery.Service{
				InstanceId:   "123",
				InstanceName: "service1",
				//InterfaceName: strings.Repeat("i", 260),
				InterfaceName: "v.io/y",
				Attrs: discovery.Attributes{
					"a": strings.Repeat("v", 260),
				},
				Addrs: []string{
					strings.Repeat("a1", 70),
					strings.Repeat("a2", 70),
				},
				Attachments: discovery.Attachments{
					"p": bytes.Repeat([]byte{1}, 260),
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("123"),
			Hash:                []byte{1, 2, 3},
			DirAddrs:            []string{"d"},
		},
		{
			Service: discovery.Service{
				InstanceId:    "456",
				InstanceName:  "service2",
				InterfaceName: "v.io/y",
				Attrs:         discovery.Attributes{"a": "v"},
				Addrs:         []string{"a"},
				Attachments:   discovery.Attachments{"p": []byte{1}},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys(strings.Repeat("k", 260)),
			Hash:                bytes.Repeat([]byte{1}, 260),
			DirAddrs: []string{
				strings.Repeat("d", 260),
			},
		},
	}

	p1, err := newMDNS("m1")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	for _, ad := range ads {
		stop, err := advertise(ctx, p1, ad)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
	}

	p2, err := newMDNS("m2")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := scanAndMatch(ctx, p2, "", ads...); err != nil {
		t.Error(err)
	}
}
