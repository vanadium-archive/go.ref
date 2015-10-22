// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ios

// The paypal gatt library, which the ble plugin currently depends on doesn't work
// in iOS, so remove the ble plugin entirely for now.

package factory

import (
	"v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/mdns"
)

var pluginFactories = map[string]func(host string) (discovery.Plugin, error){
	"mdns": mdns.New,
}
