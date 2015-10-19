// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"reflect"
	"testing"

	"v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/testdata"
)

func TestPackAddresses(t *testing.T) {
	for _, test := range testdata.PackAddressTestData {
		pack := discovery.PackAddresses(test.In)
		if !reflect.DeepEqual(pack, test.Packed) {
			t.Errorf("packed to: %v, but wanted: %v", pack, test.Packed)
		}
		unpack, err := discovery.UnpackAddresses(test.Packed)
		if err != nil {
			t.Errorf("unpacked error: %v", err)
			continue
		}
		if !reflect.DeepEqual(test.In, unpack) {
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
			t.Errorf("unpacked error: %v", err)
			continue
		}
		if algo != test.Algo || !reflect.DeepEqual(keys, test.Keys) {
			t.Errorf("unpacked to (%v, %v), but want (%v, %v)", algo, keys, test.Algo, test.Keys)
		}
	}
}
