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

const (
	// This uuids are v5 uuid generated out of band.  These constants need
	// to be accessible in all the languages that have a ble implementation
	instanceUUID      = "12db9a9c-1c7c-5560-bc6b-73a115c93413" // NewAttributeUUID("_instanceuuid")
	interfaceNameUUID = "b2cadfd4-d003-576c-acad-58b8e3a9cbc8" // NewAttributeUUID("_interfacename")
	addrsUUID         = "ad2566b7-59d8-50ae-8885-222f43f65fdc" // NewAttributeUUID("_addrs")
	encryptionUUID    = "6286d80a-adaa-519a-8a06-281a4645a607" // NewAttributeUUID("_encryption")
)

func newAdvertisment(adv discovery.Advertisement) bleAdv {
	attrs := map[string][]byte{
		instanceUUID:      adv.InstanceUuid,
		interfaceNameUUID: []byte(adv.InterfaceName),
		addrsUUID:         discovery.PackAddresses(adv.Addrs),
		encryptionUUID:    discovery.PackEncryptionKeys(adv.EncryptionAlgorithm, adv.EncryptionKeys),
	}

	for k, v := range adv.Attrs {
		hexUUID := discovery.NewAttributeUUID(k).String()
		attrs[hexUUID] = []byte(k + "=" + v)
	}
	return bleAdv{
		instanceID:  adv.InstanceUuid,
		serviceUUID: adv.ServiceUuid,
		attrs:       attrs,
	}
}

func (a *bleAdv) toDiscoveryAdvertisement() (*discovery.Advertisement, error) {
	adv := &discovery.Advertisement{
		Service: vdiscovery.Service{
			InstanceUuid: a.instanceID,
			Attrs:        make(vdiscovery.Attributes),
		},
		ServiceUuid: a.serviceUUID,
	}

	for k, v := range a.attrs {
		switch k {
		case instanceUUID:
			adv.InstanceUuid = v
		case interfaceNameUUID:
			adv.InterfaceName = string(v)
		case addrsUUID:
			adv.Addrs = discovery.UnpackAddresses(v)
		case encryptionUUID:
			adv.EncryptionAlgorithm, adv.EncryptionKeys = discovery.UnpackEncryptionKeys(v)
		default:
			parts := strings.SplitN(string(v), "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("incorrectly formatted value, %s", v)
			}
			adv.Attrs[parts[0]] = parts[1]
		}
	}
	return adv, nil
}
