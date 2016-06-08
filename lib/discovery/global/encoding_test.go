// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"reflect"
	"testing"

	"v.io/v23/discovery"
)

func TestAdSuffix(t *testing.T) {
	testCases := []discovery.Advertisement{
		{},
		{Id: discovery.AdId{1, 2, 3}},
		{Attributes: discovery.Attributes{"k": "v"}},
		{Id: discovery.AdId{1, 2, 3}, Attributes: discovery.Attributes{"k": "v"}},
	}
	for _, want := range testCases {
		encAd, err := encodeAdToSuffix(&want)
		if err != nil {
			t.Error(err)
		}
		got, err := decodeAdFromSuffix(encAd)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(*got, want) {
			t.Errorf("got %v, want %v", *got, want)
		}
	}
}
