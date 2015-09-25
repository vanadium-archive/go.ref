// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"strings"

	"fmt"
	"net/url"

	vdiscovery "v.io/v23/discovery"
	"v.io/x/ref/lib/discovery"

	"github.com/pborman/uuid"
)

type bleAdv struct {
	serviceUUID uuid.UUID
	instanceID  []byte
	attrs       map[string][]byte
}

var (
	// This uuids are v4 uuid generated out of band.  These constants need
	// to be accessible in all the languages that have a ble implementation

	// The attribute uuid for the unique service id
	instanceUUID = "f6445c7f-73fd-4b8d-98d0-c4e02b087844"

	// The attribute uuid for the interface name
	interfaceNameUUID = "d4789810-4db0-40d8-9658-92f8e304d578"

	addrUUID = "f123fb0e-770f-4e46-b8ad-aee4185ab5a1"
)

func newAdvertisment(adv discovery.Advertisement) bleAdv {
	cleanAddrs := make([]string, len(adv.Addrs))
	for i, v := range adv.Addrs {
		cleanAddrs[i] = url.QueryEscape(v)
	}
	attrs := map[string][]byte{
		instanceUUID:      adv.InstanceUuid,
		interfaceNameUUID: []byte(adv.InterfaceName),
		addrUUID:          []byte(strings.Join(cleanAddrs, "&")),
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
	out := &discovery.Advertisement{
		Service: vdiscovery.Service{
			Attrs:         vdiscovery.Attributes{},
			InterfaceName: string(a.attrs[interfaceNameUUID]),
			InstanceUuid:  a.instanceID,
		},
		ServiceUuid: a.serviceUUID,
	}
	out.Addrs = strings.Split(string(a.attrs[addrUUID]), "&")
	var err error
	for i, v := range out.Addrs {
		out.Addrs[i], err = url.QueryUnescape(v)
		if err != nil {
			return nil, err
		}
	}

	for k, v := range a.attrs {
		if k == instanceUUID || k == interfaceNameUUID || k == addrUUID {
			continue
		}
		parts := strings.SplitN(string(v), "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("incorrectly formatted value, %s", v)
		}
		out.Attrs[parts[0]] = parts[1]

	}

	return out, nil
}
