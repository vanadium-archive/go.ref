// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"net"

	"v.io/x/lib/pubsub"
)

// NewAddAddrsSetting creates the Setting to be sent to Listen to inform
// it of new addresses that have become available since the last change.
func NewAddAddrsSetting(a []net.Addr) pubsub.Setting {
	return pubsub.NewAny(NewAddrsSetting, NewAddrsSettingDesc, a)
}

// NewRmAddrsSetting creates the Setting to be sent to Listen to inform
// it of addresses that are no longer available.
func NewRmAddrsSetting(a []net.Addr) pubsub.Setting {
	return pubsub.NewAny(RmAddrsSetting, RmAddrsSettingDesc, a)
}

const (
	NewAddrsSetting     = "NewAddrs"
	NewAddrsSettingDesc = "New Addresses discovered since last change"
	RmAddrsSetting      = "RmAddrs"
	RmAddrsSettingDesc  = "Addresses that have been removed since last change"
)
