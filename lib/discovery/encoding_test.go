// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"reflect"
	"testing"

	"v.io/v23/discovery"
)

func TestValidateAttributes(t *testing.T) {
	valids := []discovery.Attributes{
		discovery.Attributes{"key": "v"},
		discovery.Attributes{"k_e.y": "v"},
		discovery.Attributes{"k!": "v"},
	}
	for i, attrs := range valids {
		if err := validateAttributes(attrs); err != nil {
			t.Errorf("[%d]: valid attributes got error: %v", i, err)
		}
	}

	invalids := []discovery.Attributes{
		discovery.Attributes{"_key": "v"},
		discovery.Attributes{"k=ey": "v"},
		discovery.Attributes{"key\n": "v"},
	}
	for i, attrs := range invalids {
		if err := validateAttributes(attrs); err == nil {
			t.Errorf("[%d]: invalid attributes didn't get error", i)
		}
	}
}

func TestPackAddresses(t *testing.T) {
	tests := [][]string{
		[]string{"a12345"},
		[]string{"a1234", "b5678", "c9012"},
		nil,
	}

	for _, test := range tests {
		pack := PackAddresses(test)
		unpack, err := UnpackAddresses(pack)
		if err != nil {
			t.Errorf("unpacked error: %v", err)
			continue
		}
		if !reflect.DeepEqual(test, unpack) {
			t.Errorf("unpacked to %v, but want %v", unpack, test)
		}
	}
}

func TestPackEncryptionKeys(t *testing.T) {
	tests := []struct {
		algo EncryptionAlgorithm
		keys []EncryptionKey
	}{
		{TestEncryption, []EncryptionKey{EncryptionKey("0123456789")}},
		{IbeEncryption, []EncryptionKey{EncryptionKey("012345"), EncryptionKey("123456"), EncryptionKey("234567")}},
		{NoEncryption, nil},
	}

	for _, test := range tests {
		pack := PackEncryptionKeys(test.algo, test.keys)
		algo, keys, err := UnpackEncryptionKeys(pack)
		if err != nil {
			t.Errorf("unpacked error: %v", err)
			continue
		}
		if algo != test.algo || !reflect.DeepEqual(keys, test.keys) {
			t.Errorf("unpacked to (%d, %v), but want (%d, %v)", algo, keys, test.algo, test.keys)
		}
	}
}
