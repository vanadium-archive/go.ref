// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !linux

package internal

import (
	"net"

	"v.io/v23/logging"
)

// GCEPublicAddress returns the public IP address of the GCE instance
// it is run from, or nil if run from anywhere else. The returned address
// is the public address of a 1:1 NAT tunnel to this host.
func GCEPublicAddress(logging.Logger) *net.IPAddr {
	return nil
}
