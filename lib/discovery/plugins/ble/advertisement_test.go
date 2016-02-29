// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"reflect"
	"testing"

	"github.com/pborman/uuid"

	idiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/discovery/plugins/ble/testdata"
)

func TestConvertingBackAndForth(t *testing.T) {
	for i, test := range testdata.ConversionTestData {
		adinfo := test.AdInfo
		serviceUuid := uuid.UUID(idiscovery.NewServiceUUID(adinfo.Ad.InterfaceName))

		bleAd := newBleAd(&adinfo)
		if !uuid.Equal(bleAd.serviceUuid, serviceUuid) {
			t.Errorf("[%d]: wanted %v, but got %v", i, serviceUuid, bleAd.serviceUuid)
		}
		if !reflect.DeepEqual(bleAd.attrs, test.GattAttrs) {
			t.Errorf("[%d]: wanted %v, but got %v", i, test.GattAttrs, bleAd.attrs)
		}

		out, err := bleAd.toAdInfo()
		if err != nil {
			t.Errorf("[%d]: unexpected error: %v", i, err)
		}
		if !reflect.DeepEqual(&adinfo, out) {
			t.Errorf("[%d]: wanted %v, but got %v", i, adinfo, out)
		}
	}
}
