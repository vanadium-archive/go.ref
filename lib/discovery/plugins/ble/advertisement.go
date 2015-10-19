// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"fmt"
	"strings"

	"github.com/pborman/uuid"

	vdiscovery "v.io/v23/discovery"

	"v.io/x/ref/lib/discovery"
)

type bleAdv struct {
	serviceUUID uuid.UUID
	instanceID  []byte
	attrs       map[string][]byte
}

func newAdvertisment(adv discovery.Advertisement) bleAdv {
	attrs := map[string][]byte{
		InstanceUUID:      adv.Service.InstanceUuid,
		InterfaceNameUUID: []byte(adv.Service.InterfaceName),
	}
	if len(adv.Service.InstanceName) > 0 {
		attrs[InstanceNameUUID] = []byte(adv.Service.InstanceName)
	}
	if len(adv.Service.Addrs) > 0 {
		attrs[AddrsUUID] = discovery.PackAddresses(adv.Service.Addrs)
	}
	if adv.EncryptionAlgorithm != discovery.NoEncryption {
		attrs[EncryptionUUID] = discovery.PackEncryptionKeys(adv.EncryptionAlgorithm, adv.EncryptionKeys)
	}

	for k, v := range adv.Service.Attrs {
		hexUUID := discovery.NewAttributeUUID(k).String()
		attrs[hexUUID] = []byte(k + "=" + v)
	}
	return bleAdv{
		instanceID:  adv.Service.InstanceUuid,
		serviceUUID: uuid.UUID(adv.ServiceUuid),
		attrs:       attrs,
	}
}

func (a *bleAdv) toDiscoveryAdvertisement() (*discovery.Advertisement, error) {
	adv := &discovery.Advertisement{
		Service: vdiscovery.Service{
			InstanceUuid: a.instanceID,
			Attrs:        make(vdiscovery.Attributes),
		},
		ServiceUuid: discovery.Uuid(a.serviceUUID),
	}

	var err error
	for k, v := range a.attrs {
		switch k {
		case InstanceUUID:
			adv.Service.InstanceUuid = v
		case InstanceNameUUID:
			adv.Service.InstanceName = string(v)
		case InterfaceNameUUID:
			adv.Service.InterfaceName = string(v)
		case AddrsUUID:
			if adv.Service.Addrs, err = discovery.UnpackAddresses(v); err != nil {
				return nil, err
			}
		case EncryptionUUID:
			if adv.EncryptionAlgorithm, adv.EncryptionKeys, err = discovery.UnpackEncryptionKeys(v); err != nil {
				return nil, err
			}
		default:
			parts := strings.SplitN(string(v), "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("incorrectly formatted value, %s", v)
			}
			adv.Service.Attrs[parts[0]] = parts[1]
		}
	}
	return adv, nil
}
