// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
)

// Plugin is the basic interface for a plugin to discovery service.
// All implementation should be goroutine-safe.
type Plugin interface {
	// Advertise advertises the advertisement. Advertising will continue until
	// the context is canceled or exceeds its deadline. done should be called
	// once when advertising is done or canceled.
	Advertise(ctx *context.T, ad Advertisement, done func()) error

	// Scan scans services that match the interface name and returns scanned
	// advertisements to the channel. An empty interface name means any service.
	// Scanning will continue until the context is canceled or exceeds its
	// deadline. done should be called once when scanning is done or canceled.
	//
	// TODO(jhahn): Pass a filter on service attributes.
	Scan(ctx *context.T, interfaceName string, ch chan<- Advertisement, done func()) error
}
