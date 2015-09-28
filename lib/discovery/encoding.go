// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"fmt"
	"strings"

	"v.io/v23/discovery"
)

// TODO(jhahn): Figure out how to overcome the size limit.

// isAttributePackage returns false if the provided attributes cannot be serialized safely.
func IsAttributePackable(attrs discovery.Attributes) bool {
	for k, v := range attrs {
		if strings.HasPrefix(k, "_") || strings.Contains(k, "=") {
			return false
		}
		if len(k)+len(v) > 254 {
			return false
		}
	}
	return true
}

// IsAddressPackable returns false if any address is larger than 250 bytes.
//
// go-mdns-sd package limits the size of each txt record to 255 bytes. We use
// 5 bytes for tag, so we limit the address to 250 bytes.
func IsAddressPackable(addrs []string) bool {
	for _, a := range addrs {
		if len(a) > 250 {
			return false
		}
	}
	return true
}

// PackAddresses packs addresses into a byte slice. If any address exceeds
// 255 bytes, it will panic.
func PackAddresses(addrs []string) []byte {
	var b bytes.Buffer
	for _, a := range addrs {
		n := len(a)
		if n > 255 {
			panic(fmt.Sprintf("too large address %d: %s", n, a))
		}
		b.WriteByte(byte(n))
		b.WriteString(a)
	}
	return b.Bytes()
}

// UnpackAddresses unpacks addresses from a byte slice.
func UnpackAddresses(data []byte) []string {
	addrs := []string{}
	for off := 0; off < len(data); {
		n := int(data[off])
		off++
		addrs = append(addrs, string(data[off:off+n]))
		off += n
	}
	return addrs
}

// PackEncryptionKeys packs keys into a byte slice.
func PackEncryptionKeys(algo EncryptionAlgorithm, keys []EncryptionKey) []byte {
	var b bytes.Buffer
	b.WriteByte(byte(algo))
	for _, k := range keys {
		n := len(k)
		if n > 255 {
			panic(fmt.Sprintf("too large key %d", n))
		}
		b.WriteByte(byte(n))
		b.Write(k)
	}
	return b.Bytes()
}

// UnpackEncryptionKeys unpacks keys from a byte slice.
func UnpackEncryptionKeys(data []byte) (EncryptionAlgorithm, []EncryptionKey) {
	algo := EncryptionAlgorithm(data[0])
	keys := []EncryptionKey{}
	for off := 1; off < len(data); {
		n := int(data[off])
		off++
		keys = append(keys, EncryptionKey(data[off:off+n]))
		off += n
	}
	return algo, keys
}
