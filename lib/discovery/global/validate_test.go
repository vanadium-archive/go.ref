// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"testing"

	"v.io/v23/discovery"
)

func TestValidateService(t *testing.T) {
	tests := []struct {
		service discovery.Service
		valid   bool
	}{
		{
			discovery.Service{
				InstanceId: "123",
				Addrs:      []string{"/h:123/x"},
			},
			true,
		},
		{
			discovery.Service{
				InstanceId: "\x01\x81", // Invalid UTF-8.
				Addrs:      []string{"/h:123/x"},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId: "123/456", // Include '/'.
				Addrs:      []string{"/h:123/y"},
			},
			false,
		},
		{
			discovery.Service{ // No addresses.
				InstanceId: "i1",
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a", // Has interface name.
				Addrs:         []string{"/h:123/x"},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId: "123",
				Addrs:      []string{"/h:123/x"},
				Attrs: discovery.Attributes{ // Has attributes.
					"k": "v",
				},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId: "123",
				Addrs:      []string{"/h:123/x"},
				Attachments: discovery.Attachments{ // Has attachments.
					"k": []byte{1},
				},
			},
			false,
		},
	}

	for i, test := range tests {
		err := validateService(&test.service)
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
