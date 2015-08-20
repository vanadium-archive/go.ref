// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security/access"
)

// Advertise implements discovery.Advertiser.
func (ds *ds) Advertise(ctx *context.T, service discovery.Service, perms access.Permissions) error {
	return nil
}
