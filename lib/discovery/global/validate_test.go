// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"testing"

	"v.io/v23/discovery"
)

func TestValidateAd(t *testing.T) {
	tests := []struct {
		ad    discovery.Advertisement
		valid bool
	}{
		{
			discovery.Advertisement{
				Id:         discovery.AdId{1, 2, 3},
				Addresses:  []string{"/h:123/x"},
				Attributes: discovery.Attributes{"k": "v"},
			},
			true,
		},
		{
			discovery.Advertisement{
				Id:        discovery.AdId{}, // Invalid id.
				Addresses: []string{"/h:123/x"},
			},
			false,
		},
		{
			discovery.Advertisement{ // No addresses.
				Id: discovery.AdId{1, 2, 3},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a", // Has interface name.
				Addresses:     []string{"/h:123/x"},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:        discovery.AdId{1, 2, 3},
				Addresses: []string{"/h:123/x"},
				Attachments: discovery.Attachments{ // Has attachments.
					"k": []byte{1},
				},
			},
			false,
		},
	}

	for i, test := range tests {
		err := validateAd(&test.ad)
		if test.valid {
			if err != nil {
				t.Errorf("[%d]: unexpected error: %v", i, err)
			}
		} else {
			if err == nil {
				t.Errorf("[%d]: expected an error; but got none", i)
			}
		}
	}
}
