// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"crypto/rand"

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
