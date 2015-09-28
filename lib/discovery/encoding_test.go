// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"reflect"
	"strings"
	"testing"

	"v.io/v23/discovery"
)

func TestAttributePackable(t *testing.T) {
	tests := []struct {
		addrs discovery.Attributes
		want  bool
	}{
		{discovery.Attributes{"k": "v"}, true},
		{discovery.Attributes{"_k": "v"}, false},
		{discovery.Attributes{"k=": "v"}, false},
		{discovery.Attributes{strings.Repeat("k", 100): strings.Repeat("v", 154)}, true},
		{discovery.Attributes{strings.Repeat("k", 100): strings.Repeat("v", 155)}, false},
	}
	for i, test := range tests {
		if got := IsAttributePackable(test.addrs); got != test.want {
			t.Errorf("[%d]: packable %v, but want %v", i, got, test.want)
		}
	}
}

func TestAddressPackable(t *testing.T) {
	tests := []struct {
		addrs []string
		want  bool
	}{
		{[]string{strings.Repeat("a", 250)}, true},
		{[]string{strings.Repeat("a", 10), strings.Repeat("a", 251)}, false},
	}
	for i, test := range tests {
		if got := IsAddressPackable(test.addrs); got != test.want {
			t.Errorf("[%d]: packable %v, but want %v", i, got, test.want)
		}
	}
}

func TestPackAddresses(t *testing.T) {
	tests := [][]string{
		[]string{"a12345"},
		[]string{"a1234", "b5678", "c9012"},
		[]string{},
	}

	for _, test := range tests {
		pack := PackAddresses(test)
		unpack := UnpackAddresses(pack)
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
		{NoEncryption, []EncryptionKey{}},
	}

	for _, test := range tests {
		pack := PackEncryptionKeys(test.algo, test.keys)
		algo, keys := UnpackEncryptionKeys(pack)
		if algo != test.algo || !reflect.DeepEqual(keys, test.keys) {
			t.Errorf("unpacked to (%d, %v), but want (%d, %v)", algo, keys, test.algo, test.keys)
		}
	}
}
