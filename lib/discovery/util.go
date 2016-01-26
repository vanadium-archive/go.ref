// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"sort"

	"v.io/v23/discovery"
)

func copyService(service *discovery.Service) discovery.Service {
	copied := discovery.Service{
		InstanceId:    service.InstanceId,
		InstanceName:  service.InstanceName,
		InterfaceName: service.InterfaceName,
	}
	if n := len(service.Attrs); n > 0 {
		copied.Attrs = make(discovery.Attributes, n)
		for k, v := range service.Attrs {
			copied.Attrs[k] = v
		}
	}
	copied.Addrs = make([]string, len(service.Addrs))
	copy(copied.Addrs, service.Addrs)
	return copied
}

func newInstanceId() (string, error) {
	// Generate a random 119 bit ascii string, which is similar to 122 bit random UUID.
	const idLen = 17

	instanceId := make([]byte, idLen)
	if _, err := rand.Read(instanceId); err != nil {
		return "", err
	}
	for i := 0; i < idLen; i++ {
		instanceId[i] &= 0x7f
	}
	return string(instanceId), nil
}

func hashAdvertisement(ad *Advertisement) {
	w := func(w io.Writer, data []byte) {
		sum := sha256.Sum256(data)
		w.Write(sum[:])
	}

	hasher := sha256.New()

	service := ad.Service
	w(hasher, []byte(service.InstanceId))
	w(hasher, []byte(service.InstanceName))
	w(hasher, []byte(service.InterfaceName))

	field := sha256.New()
	for _, addr := range service.Addrs {
		w(field, []byte(addr))
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	if n := len(service.Attrs); n > 0 {
		keys := make([]string, 0, n)
		for k, _ := range service.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w(field, []byte(k))
			w(field, []byte(service.Attrs[k]))
		}
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	if n := len(service.Attachments); n > 0 {
		keys := make([]string, 0, n)
		for k, _ := range service.Attachments {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w(field, []byte(k))
			w(field, []byte(service.Attachments[k]))
		}
	}
	hasher.Write(field.Sum(nil))

	field.Reset()
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, ad.EncryptionAlgorithm)
	w(field, buf.Bytes())
	for _, key := range ad.EncryptionKeys {
		w(field, []byte(key))
	}
	hasher.Write(field.Sum(nil))

	// We use the first 8 bytes to reduce the advertise packet size.
	ad.Hash = hasher.Sum(nil)[:8]
}
