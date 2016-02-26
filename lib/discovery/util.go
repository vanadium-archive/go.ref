// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"sort"
)

// Hash hashes the advertisement.
func HashAd(adinfo *AdInfo) {
	w := func(w io.Writer, data []byte) {
		sum := sha256.Sum256(data)
		w.Write(sum[:])
	}

	hasher := sha256.New()

	w(hasher, adinfo.Ad.Id[:])
	w(hasher, []byte(adinfo.Ad.InterfaceName))

	field := sha256.New()
	for _, addr := range adinfo.Ad.Addresses {
		w(field, []byte(addr))
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	if n := len(adinfo.Ad.Attributes); n > 0 {
		keys := make([]string, 0, n)
		for k, _ := range adinfo.Ad.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w(field, []byte(k))
			w(field, []byte(adinfo.Ad.Attributes[k]))
		}
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	if n := len(adinfo.Ad.Attachments); n > 0 {
		keys := make([]string, 0, n)
		for k, _ := range adinfo.Ad.Attachments {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w(field, []byte(k))
			w(field, []byte(adinfo.Ad.Attachments[k]))
		}
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, adinfo.EncryptionAlgorithm)
	w(field, buf.Bytes())
	for _, key := range adinfo.EncryptionKeys {
		w(field, []byte(key))
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	for _, addr := range adinfo.DirAddrs {
		w(field, []byte(addr))
	}
	hasher.Write(field.Sum(nil))

	// We use the first 8 bytes to reduce the advertise packet size.
	copy(adinfo.Hash[:], hasher.Sum(nil))
}
