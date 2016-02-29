// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/testdata"
	"v.io/x/ref/lib/security/bcrypter"
)

func TestPackAddresses(t *testing.T) {
	for _, test := range testdata.PackAddressTestData {
		pack := discovery.PackAddresses(test.In)
		if !reflect.DeepEqual(pack, test.Packed) {
			t.Errorf("packed to: %v, but wanted: %v", pack, test.Packed)
		}
		unpack, err := discovery.UnpackAddresses(test.Packed)
		if err != nil {
			t.Errorf("unpack error: %v", err)
			continue
		}
		if !reflect.DeepEqual(unpack, test.In) {
			t.Errorf("unpacked to %v, but want %v", unpack, test.In)
		}
	}
}

func TestPackEncryptionKeys(t *testing.T) {
	for _, test := range testdata.PackEncryptionKeysTestData {
		pack := discovery.PackEncryptionKeys(test.Algo, test.Keys)

		if !reflect.DeepEqual(pack, test.Packed) {
			t.Errorf("packed to: %v, but wanted: %v", pack, test.Packed)
		}

		algo, keys, err := discovery.UnpackEncryptionKeys(test.Packed)
		if err != nil {
			t.Errorf("unpack error: %v", err)
			continue
		}
		if algo != test.Algo || !reflect.DeepEqual(keys, test.Keys) {
			t.Errorf("unpacked to (%v, %v), but want (%v, %v)", algo, keys, test.Algo, test.Keys)
		}
	}
}

func TestEncodeWireCiphertext(t *testing.T) {
	rand := rand.New(rand.NewSource(0))
	for i := 0; i < 1; i++ {
		v, ok := quick.Value(reflect.TypeOf(bcrypter.WireCiphertext{}), rand)
		if !ok {
			t.Fatal("failed to populate value")
		}
		wctext := v.Interface().(bcrypter.WireCiphertext)
		// Make reflect.DeepEqual happy in comparing nil and empty.
		if wctext.Bytes == nil {
			wctext.Bytes = make(map[string][]byte)
		}

		encoded := discovery.EncodeWireCiphertext(&wctext)
		decoded, err := discovery.DecodeWireCiphertext(encoded)
		if err != nil {
			t.Errorf("decoded error: %v", err)
			continue
		}
		if !reflect.DeepEqual(decoded, &wctext) {
			t.Errorf("decoded to %v, but want %v", *decoded, wctext)
		}
	}
}
