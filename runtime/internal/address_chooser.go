// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"net"

	"v.io/v23/logging"
	"v.io/v23/rpc"
)

type addressChooser struct {
	logger    logging.Logger
	ipChooser IPAddressChooser
}

func (c *addressChooser) ChooseAddresses(protocol string, candidates []net.Addr) ([]net.Addr, error) {
	if ipaddr := CloudVMPublicAddress(); ipaddr != nil {
		c.logger.Infof("CloudVM public IP address: %v", ipaddr)
		return []net.Addr{ipaddr}, nil
	}
	return c.ipChooser.ChooseAddresses(protocol, candidates)
}

// NewAddressChooser will return the public IP of process if the process is
// being hosted by a cloud service provider (e.g. Google Compute Engine,
// Amazon EC2), and if not will be the same as IPAddressChooser.
func NewAddressChooser(logger logging.Logger) rpc.AddressChooser {
	if HasPublicIP(logger) {
		return IPAddressChooser{}
	}
	return &addressChooser{logger: logger}
}
