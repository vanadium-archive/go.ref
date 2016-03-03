// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mdns

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/testutil"
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
	return newWithLoopback(nil, host, testPort, true)
}

func encryptionKeys(key string) []idiscovery.EncryptionKey {
	return []idiscovery.EncryptionKey{idiscovery.EncryptionKey(fmt.Sprintf("key:%x", key))}
}

func TestBasic(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	adinfos := []idiscovery.AdInfo{
		{
			Ad: discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/x",
				Addresses: []string{
					"/@6@wsh@foo.com:1234@@/x",
				},
				Attributes: discovery.Attributes{
					"a": "a1234",
					"b": "b1234",
				},
				Attachments: discovery.Attachments{
					"a": []byte{11, 12, 13},
					"p": []byte{21, 22, 23},
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("123"),
			Hash:                idiscovery.AdHash{1, 2, 3},
			DirAddrs: []string{
				"/@6@wsh@foo.com:1234@@/d",
			},
		},
		{
			Ad: discovery.Advertisement{
				Id:            discovery.AdId{4, 5, 6},
				InterfaceName: "v.io/x",
				Addresses: []string{
					"/@6@wsh@bar.com:1234@@/x",
				},
				Attributes: discovery.Attributes{
					"a": "a5678",
					"b": "b5678",
				},
				Attachments: discovery.Attachments{
					"a": []byte{31, 32, 33},
					"p": []byte{41, 42, 43},
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("456"),
			Hash:                idiscovery.AdHash{4, 5, 6},
			DirAddrs: []string{
				"/@6@wsh@bar.com:1234@@/d",
			},
		},
		{
			Ad: discovery.Advertisement{
				Id:            discovery.AdId{7, 8, 9},
				InterfaceName: "v.io/y",
				Addresses: []string{
					"/@6@wsh@foo.com:1234@@/y",
					"/@6@wsh@bar.com:1234@@/y",
				},
				Attributes: discovery.Attributes{
					"c": "c1234",
					"d": "d1234",
				},
				Attachments: discovery.Attachments{
					"c": []byte{51, 52, 53},
					"p": []byte{61, 62, 63},
				},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys("789"),
			Hash:                idiscovery.AdHash{7, 8, 9},
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
	for _, adinfo := range adinfos {
		stop, err := testutil.Advertise(ctx, p1, &adinfo)
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
	if err := testutil.ScanAndMatch(ctx, p2, "v.io/x", adinfos[0], adinfos[1]); err != nil {
		t.Error(err)
	}
	if err := testutil.ScanAndMatch(ctx, p2, "v.io/y", adinfos[2]); err != nil {
		t.Error(err)
	}
	if err := testutil.ScanAndMatch(ctx, p2, "", adinfos...); err != nil {
		t.Error(err)
	}
	if err := testutil.ScanAndMatch(ctx, p2, "v.io/z"); err != nil {
		t.Error(err)
	}

	// Make sure it is not discovered when advertising is stopped.
	stops[0]()
	if err := testutil.ScanAndMatch(ctx, p2, "v.io/x", adinfos[1]); err != nil {
		t.Error(err)
	}
	if err := testutil.ScanAndMatch(ctx, p2, "", adinfos[1], adinfos[2]); err != nil {
		t.Error(err)
	}

	// Open a new scan channel and consume expected advertisements first.
	scanCh, scanStop, err := testutil.Scan(ctx, p2, "v.io/y")
	if err != nil {
		t.Error(err)
	}
	defer scanStop()

	adinfo := *<-scanCh
	if !testutil.MatchFound([]idiscovery.AdInfo{adinfo}, adinfos[2]) {
		t.Errorf("Unexpected scan: %v, but want %v", adinfo, adinfos[2])
	}

	// Make sure scan returns the lost advertisement when advertising is stopped.
	stops[2]()

	adinfo = *<-scanCh
	if !testutil.MatchLost([]idiscovery.AdInfo{adinfo}, adinfos[2]) {
		t.Errorf("Unexpected scan: %v, but want %v as lost", adinfo, adinfos[2])
	}

	// Stop advertising the remaining one; Shouldn't discover anything.
	stops[1]()
	if err := testutil.ScanAndMatch(ctx, p2, ""); err != nil {
		t.Error(err)
	}
}

func TestLargeTxt(t *testing.T) {
	ctx, shutdown := test.TestContext()
	defer shutdown()

	adinfos := []idiscovery.AdInfo{
		{
			Ad: discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: strings.Repeat("i", 260),
				Addresses: []string{
					strings.Repeat("a1", 70),
					strings.Repeat("a2", 70),
				},
				Attributes: discovery.Attributes{
					"a": strings.Repeat("v", 260),
				},
				Attachments: discovery.Attachments{
					"p": bytes.Repeat([]byte{1}, 260),
				},
			},
		},
		{
			Ad: discovery.Advertisement{
				Id:            discovery.AdId{4, 5, 6},
				InterfaceName: "v.io/y",
				Addresses:     []string{"a"},
				Attributes:    discovery.Attributes{},
				Attachments:   discovery.Attachments{},
			},
			EncryptionAlgorithm: idiscovery.TestEncryption,
			EncryptionKeys:      encryptionKeys(strings.Repeat("k", 260)),
			DirAddrs: []string{
				strings.Repeat("d1", 130),
				strings.Repeat("d2", 130),
			},
		},
	}

	p1, err := newMDNS("m1")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	for _, adinfo := range adinfos {
		stop, err := testutil.Advertise(ctx, p1, &adinfo)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
	}

	p2, err := newMDNS("m2")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := testutil.ScanAndMatch(ctx, p2, "", adinfos...); err != nil {
		t.Error(err)
	}
}
