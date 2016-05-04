// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/pborman/uuid"

	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
)

type bleAd struct {
	serviceUuid uuid.UUID
	attrs       map[string][]byte
}

func newBleAd(adinfo *idiscovery.AdInfo) bleAd {
	attrs := map[string][]byte{
		IdUuid:            adinfo.Ad.Id[:],
		InterfaceNameUuid: []byte(adinfo.Ad.InterfaceName),
		AddressesUuid:     idiscovery.PackAddresses(adinfo.Ad.Addresses),
		HashUuid:          adinfo.Hash[:],
	}
	for k, v := range adinfo.Ad.Attributes {
		uuid := uuid.UUID(idiscovery.NewAttributeUUID(k)).String()
		attrs[uuid] = []byte(k + "=" + v)
	}
	for k, v := range adinfo.Ad.Attachments {
		k = AttachmentNamePrefix + k
		uuid := uuid.UUID(idiscovery.NewAttributeUUID(k)).String()
		var buf bytes.Buffer
		buf.WriteString(k)
		buf.WriteString("=")
		buf.Write(v)
		attrs[uuid] = buf.Bytes()
	}
	if adinfo.EncryptionAlgorithm != idiscovery.NoEncryption {
		attrs[EncryptionUuid] = idiscovery.PackEncryptionKeys(adinfo.EncryptionAlgorithm, adinfo.EncryptionKeys)
	}
	if len(adinfo.DirAddrs) > 0 {
		attrs[DirAddrsUuid] = idiscovery.PackAddresses(adinfo.DirAddrs)
	}
	return bleAd{
		serviceUuid: uuid.UUID(idiscovery.NewServiceUUID(adinfo.Ad.InterfaceName)),
		attrs:       attrs,
	}
}

func (ad *bleAd) toAdInfo() (*idiscovery.AdInfo, error) {
	adinfo := &idiscovery.AdInfo{
		Ad: discovery.Advertisement{
			Attributes:  make(discovery.Attributes),
			Attachments: make(discovery.Attachments),
		},
	}

	var err error
	for k, v := range ad.attrs {
		switch k {
		case IdUuid:
			if len(v) != len(adinfo.Ad.Id) {
				return nil, fmt.Errorf("invalid id: %v", v)
			}
			copy(adinfo.Ad.Id[:], v)
		case InterfaceNameUuid:
			adinfo.Ad.InterfaceName = string(v)
		case AddressesUuid:
			if adinfo.Ad.Addresses, err = idiscovery.UnpackAddresses(v); err != nil {
				return nil, err
			}
		case EncryptionUuid:
			if adinfo.EncryptionAlgorithm, adinfo.EncryptionKeys, err = idiscovery.UnpackEncryptionKeys(v); err != nil {
				return nil, err
			}
		case HashUuid:
			if len(v) != len(adinfo.Hash) {
				return nil, fmt.Errorf("invalid hash: %v", v)
			}
			copy(adinfo.Hash[:], v)
		case DirAddrsUuid:
			if adinfo.DirAddrs, err = idiscovery.UnpackAddresses(v); err != nil {
				return nil, err
			}
		default:
			p := bytes.SplitN(v, []byte{'='}, 2)
			if len(p) != 2 {
				return nil, fmt.Errorf("invalid attributes: %v", v)
			}
			name := string(p[0])
			if strings.HasPrefix(name, AttachmentNamePrefix) {
				adinfo.Ad.Attachments[name[len(AttachmentNamePrefix):]] = p[1]
			} else {
				adinfo.Ad.Attributes[name] = string(p[1])
			}
		}
	}
	return adinfo, nil
}
