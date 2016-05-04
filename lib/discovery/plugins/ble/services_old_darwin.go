// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import "github.com/paypal/gatt"

var gattOptions = []gatt.Option{
	gatt.MacDeviceRole(gatt.CentralManager),
}

func addDefaultServices(string) map[string]*gatt.Service {
	return map[string]*gatt.Service{}
}
