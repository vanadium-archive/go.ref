// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"math/rand"
	"reflect"
	"testing"

	"v.io/v23/discovery"
	idiscovery "v.io/x/ref/lib/discovery"
)

func TestEncode(t *testing.T) {
	rand := rand.New(rand.NewSource(0))
	randBytes := func(n int) []byte {
		p := make([]byte, rand.Intn(n))
		rand.Read(p)
		return p
	}
	randString := func(n int) string {
		return string(randBytes(n))
	}

	for i := 0; i < 10; i++ {
		adinfo := idiscovery.AdInfo{}

		// Populate adinfo manually to test large payloads. testing.quick.Value()
		// fills only with small values.
		copy(adinfo.Ad.Id[:], randBytes(32))
		adinfo.Ad.InterfaceName = randString(128)
		adinfo.Ad.Addresses = make([]string, rand.Intn(3)+1)
		for i, _ := range adinfo.Ad.Addresses {
			adinfo.Ad.Addresses[i] = randString(128)
		}

		if n := rand.Intn(5); n > 0 {
			adinfo.Ad.Attributes = make(discovery.Attributes, n)
			for i := 0; i < n; i++ {
				adinfo.Ad.Attributes[randString(16)] = randString(256)
			}
		}
		if n := rand.Intn(5); n > 0 {
			adinfo.Ad.Attachments = make(discovery.Attachments, n)
			for i := 0; i < n; i++ {
				adinfo.Ad.Attachments[randString(16)] = randBytes(256)
			}
		}

		adinfo.EncryptionAlgorithm = idiscovery.EncryptionAlgorithm(rand.Intn(3))
		if adinfo.EncryptionAlgorithm != idiscovery.NoEncryption {
			adinfo.EncryptionKeys = make([]idiscovery.EncryptionKey, rand.Intn(3)+1)
			for i, _ := range adinfo.EncryptionKeys {
				adinfo.EncryptionKeys[i] = randBytes(128)
			}
		}

		copy(adinfo.Hash[:], randBytes(16))
		adinfo.TimestampNs = rand.Int63()

		adinfo.DirAddrs = make([]string, rand.Intn(3)+1)
		for i, _ := range adinfo.DirAddrs {
			adinfo.DirAddrs[i] = randString(128)
		}

		cs, err := encodeAdInfo(&adinfo)
		if err != nil {
			t.Errorf("encode failed: %v", err)
			continue
		}

		if len(adinfo.Ad.Attachments) > 0 {
			adinfo.Status = idiscovery.AdPartiallyReady
		} else {
			adinfo.DirAddrs = nil
			adinfo.Status = idiscovery.AdReady
		}
		adinfo.Ad.Attachments = nil

		decoded, err := decodeAdInfo(cs)
		if err != nil {
			t.Errorf("decode failed: %v", err)
			continue
		}

		if !reflect.DeepEqual(decoded, &adinfo) {
			t.Errorf("decoded to %#v, but want %#v", *decoded, adinfo)
		}
	}
}
