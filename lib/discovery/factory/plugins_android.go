// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build android

package factory

import (
	"fmt"

	"v.io/v23/context"

	idiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/mdns"
)

func init() {
	pluginFactories = pluginFactoryMap{
		"mdns": mdns.New,

		// The actual plugin factory will be registered during Vanadium
		// Android runtime initialization.
		"ble": func(*context.T, string) (idiscovery.Plugin, error) {
			return nil, fmt.Errorf("ble factory not initalized")
		},
	}
}
