// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/pborman/uuid"

	vdiscovery "v.io/v23/discovery"

	"v.io/x/ref/lib/discovery"
)

type bleAdv struct {
	serviceUuid uuid.UUID
	attrs       map[string][]byte
}

func newBleAdvertisment(serviceUuid uuid.UUID, adv discovery.Advertisement) bleAdv {
	attrs := map[string][]byte{
		InstanceIdUuid:    []byte(adv.Service.InstanceId),
		InterfaceNameUuid: []byte(adv.Service.InterfaceName),
		HashUuid:          adv.Hash,
	}
	if len(adv.Service.InstanceName) > 0 {
		attrs[InstanceNameUuid] = []byte(adv.Service.InstanceName)
	}
	if len(adv.Service.Addrs) > 0 {
		attrs[AddrsUuid] = discovery.PackAddresses(adv.Service.Addrs)
	}
	for k, v := range adv.Service.Attrs {
		uuid := uuid.UUID(discovery.NewAttributeUUID(k)).String()
		attrs[uuid] = []byte(k + "=" + v)
	}
	for k, v := range adv.Service.Attachments {
		k = AttachmentNamePrefix + k
		uuid := uuid.UUID(discovery.NewAttributeUUID(k)).String()
		attrs[uuid] = append([]byte(k+"="), v...)
	}
	if adv.EncryptionAlgorithm != discovery.NoEncryption {
		attrs[EncryptionUuid] = discovery.PackEncryptionKeys(adv.EncryptionAlgorithm, adv.EncryptionKeys)
	}
	if len(adv.DirAddrs) > 0 {
		attrs[DirAddrsUuid] = discovery.PackAddresses(adv.DirAddrs)
	}
	return bleAdv{
		serviceUuid: serviceUuid,
		attrs:       attrs,
	}
}

func (a *bleAdv) toDiscoveryAdvertisement() (*discovery.Advertisement, error) {
	adv := &discovery.Advertisement{
		Service: vdiscovery.Service{
			Attrs:       make(vdiscovery.Attributes),
			Attachments: make(vdiscovery.Attachments),
		},
	}

	var err error
	for k, v := range a.attrs {
		switch k {
		case InstanceIdUuid:
			adv.Service.InstanceId = string(v)
		case InstanceNameUuid:
			adv.Service.InstanceName = string(v)
		case InterfaceNameUuid:
			adv.Service.InterfaceName = string(v)
		case AddrsUuid:
			if adv.Service.Addrs, err = discovery.UnpackAddresses(v); err != nil {
				return nil, err
			}
		case EncryptionUuid:
			if adv.EncryptionAlgorithm, adv.EncryptionKeys, err = discovery.UnpackEncryptionKeys(v); err != nil {
				return nil, err
			}
		case HashUuid:
			adv.Hash = v
		case DirAddrsUuid:
			if adv.DirAddrs, err = discovery.UnpackAddresses(v); err != nil {
				return nil, err
			}
		default:
			p := bytes.SplitN(v, []byte{'='}, 2)
			if len(p) != 2 {
				return nil, fmt.Errorf("incorrectly formatted value, %v", v)
			}
			name := string(p[0])
			if strings.HasPrefix(name, AttachmentNamePrefix) {
				adv.Service.Attachments[name[len(AttachmentNamePrefix):]] = p[1]
			} else {
				adv.Service.Attrs[name] = string(p[1])
			}
		}
	}
	return adv, nil
}
