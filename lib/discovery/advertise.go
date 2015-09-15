// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

var (
	errNoInterfaceName = verror.Register(pkgPath+".errNoInterfaceName", verror.NoRetry, "{1:}{2:} interface name not provided")
	errNoAddresses     = verror.Register(pkgPath+".errNoAddress", verror.NoRetry, "{1:}{2:} address not provided")
)

// Advertise implements discovery.Advertiser.
//
// TODO(jhahn): Handle ACL.
func (ds *ds) Advertise(ctx *context.T, service discovery.Service, perms access.Permissions) error {
	if len(service.InterfaceName) == 0 {
		return verror.New(errNoInterfaceName, ctx)
	}
	if len(service.Addrs) == 0 {
		return verror.New(errNoAddresses, ctx)
	}

	if len(service.InstanceUuid) == 0 {
		service.InstanceUuid = NewInstanceUUID()
	}
	ad := &Advertisement{
		ServiceUuid: NewServiceUUID(service.InterfaceName),
		Service:     service,
	}
	ctx, cancel := context.WithCancel(ctx)
	for _, plugin := range ds.plugins {
		err := plugin.Advertise(ctx, ad)
		if err != nil {
			cancel()
			return err
		}
	}
	return nil
}
