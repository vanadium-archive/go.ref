// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import "v.io/v23/context"

type dummyDriver struct{}

func (dummyDriver) AddService(uuid string, characteristics map[string][]byte) error { return nil }
func (dummyDriver) RemoveService(uuid string)                                       {}
func (dummyDriver) StartScan(uuids []string, baseUuid, maskUUid string, handler ScanHandler) error {
	return nil
}
func (dummyDriver) StopScan()           {}
func (dummyDriver) DebugString() string { return "BLE not available" }

func newDriver(ctx *context.T, host string) (Driver, error) {
	// TODO(jhahn): Add a real driver with CoreBluetooth.
	return dummyDriver{}, nil
}
