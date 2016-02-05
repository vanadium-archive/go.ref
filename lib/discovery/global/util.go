// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"github.com/pborman/uuid"

	"v.io/v23/discovery"
)

func copyService(service *discovery.Service) discovery.Service {
	copied := discovery.Service{InstanceId: service.InstanceId}
	copied.Addrs = make([]string, len(service.Addrs))
	copy(copied.Addrs, service.Addrs)
	return copied
}

func newInstanceId() string {
	return uuid.NewRandom().String()
}
