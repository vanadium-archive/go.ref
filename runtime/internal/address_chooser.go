// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"net"
	"sync"

	"v.io/v23/logging"
	"v.io/v23/rpc"
)

type addressChooser struct {
	logger               logging.Logger
	gcePublicAddressOnce sync.Once
	gcePublicAddress     net.Addr
	ipChooser            IPAddressChooser
}

func (c *addressChooser) setGCEPublicAddress() {
	c.gcePublicAddressOnce.Do(func() {
		c.gcePublicAddress = GCEPublicAddress(c.logger)
	})
}

func (c *addressChooser) ChooseAddresses(protocol string, candidates []net.Addr) ([]net.Addr, error) {
	c.setGCEPublicAddress() // Blocks till the address is set
	if c.gcePublicAddress == nil {
		return c.ipChooser.ChooseAddresses(protocol, candidates)
	}
	return []net.Addr{c.gcePublicAddress}, nil
}

// NewAddressChooser will return the public IP of process if the process is
// is being hosted by a cloud service provider (e.g. Google Compute Engine,
// Amazon EC2), and if not will be the same as IPAddressChooser.
func NewAddressChooser(logger logging.Logger) rpc.AddressChooser {
	if HasPublicIP(logger) {
		return IPAddressChooser{}
	}
	// Our address is private, so we test for running on GCE and for its 1:1 NAT
	// configuration. GCEPublicAddress returns a non-nil addr if we are
	// running on GCE/AWS.
	//
	// GCEPublicAddress can unforunately take up to 1 second to determine that the
	// external address (see https://github.com/vanadium/issues/issues/776).
	//
	// So NewAddressChooser fires it up in a goroutine and returns immediately,
	// thus avoiding any blockage till the AddressChooser is actually invoked.
	//
	// I apologize for the existence of this code! It is ugly, so if you have any
	// suggestions please do share. Ideally, the operation to "detect whether the
	// process is running under an Amazon EC2 instance" wouldn't block for a
	// timeout of 1 second and we can do away with this mess.
	ret := &addressChooser{logger: logger}
	go ret.setGCEPublicAddress()
	return ret
}
