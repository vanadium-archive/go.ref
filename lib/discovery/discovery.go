// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"github.com/pborman/uuid"

	"v.io/v23/discovery"
)

const pkgPath = "v.io/x/ref/runtime/internal/discovery"

// ds is an implementation of discovery.T.
type ds struct {
	plugins []Plugin
}

// Advertisement holds a set of service properties to advertise.
type Advertisement struct {
	discovery.Service

	// The service UUID to advertise.
	ServiceUuid uuid.UUID

	// TODO(jhahn): Add proximity.
	// TODO(jhahn): Use proximity for Lost.
	Lost bool
}

// TODO(jhahn): Need a better API.
func New(plugins []Plugin) discovery.T {
	ds := &ds{plugins: make([]Plugin, len(plugins))}
	copy(ds.plugins, plugins)
	return ds
}
