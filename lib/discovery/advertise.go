// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
	"v.io/v23/verror"
)

var (
	errNoInterfaceName       = verror.Register(pkgPath+".errNoInterfaceName", verror.NoRetry, "{1:}{2:} interface name not provided")
	errNotPackableAttributes = verror.Register(pkgPath+".errNotPackableAttributes", verror.NoRetry, "{1:}{2:} attribute not packable")
	errNoAddresses           = verror.Register(pkgPath+".errNoAddress", verror.NoRetry, "{1:}{2:} address not provided")
	errNotPackableAddresses  = verror.Register(pkgPath+".errNotPackableAddresses", verror.NoRetry, "{1:}{2:} address not packable")
)

// Advertise implements discovery.Advertiser.
//
// TODO(jhahn): Handle ACL.
func (ds *ds) Advertise(ctx *context.T, service discovery.Service, perms []security.BlessingPattern) error {
	if len(service.InterfaceName) == 0 {
		return verror.New(errNoInterfaceName, ctx)
	}
	if len(service.Addrs) == 0 {
		return verror.New(errNoAddresses, ctx)
	}
	if err := validateAttributes(service.Attrs); err != nil {
		return err
	}

	if len(service.InstanceUuid) == 0 {
		service.InstanceUuid = NewInstanceUUID()
	}

	ad := Advertisement{
		ServiceUuid: NewServiceUUID(service.InterfaceName),
		Service:     service,
	}
	if err := encrypt(&ad, perms); err != nil {
		return err
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
