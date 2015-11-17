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
	serviceUuid uuid.UUID
	attrs       map[string][]byte
}

func newAdvertisment(adv discovery.Advertisement) bleAdv {
	attrs := map[string][]byte{
		InstanceIdUuid:    []byte(adv.Service.InstanceId),
		InterfaceNameUuid: []byte(adv.Service.InterfaceName),
	}
	if len(adv.Service.InstanceName) > 0 {
		attrs[InstanceNameUuid] = []byte(adv.Service.InstanceName)
	}
	if len(adv.Service.Addrs) > 0 {
		attrs[AddrsUuid] = discovery.PackAddresses(adv.Service.Addrs)
	}
	if adv.EncryptionAlgorithm != discovery.NoEncryption {
		attrs[EncryptionUuid] = discovery.PackEncryptionKeys(adv.EncryptionAlgorithm, adv.EncryptionKeys)
	}
	for k, v := range adv.Service.Attrs {
		uuid := uuid.UUID(discovery.NewAttributeUUID(k)).String()
		attrs[uuid] = []byte(k + "=" + v)
	}
	return bleAdv{
		serviceUuid: uuid.UUID(adv.ServiceUuid),
		attrs:       attrs,
	}
}

func (a *bleAdv) toDiscoveryAdvertisement() (*discovery.Advertisement, error) {
	adv := &discovery.Advertisement{
		Service:     vdiscovery.Service{Attrs: make(vdiscovery.Attributes)},
		ServiceUuid: discovery.Uuid(a.serviceUuid),
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
