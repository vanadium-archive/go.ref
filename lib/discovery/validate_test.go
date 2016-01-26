// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

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
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attrs: discovery.Attributes{
					"key":   "v",
					"k_e.y": "v\u0ac0",
					"k!":    "v\n",
				},
				Attachments: discovery.Attachments{
					"key": []byte{1, 2, 3},
					"k!":  []byte{4, 5, 6},
				},
			},
			true,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attachments: discovery.Attachments{
					"k": nil,
				},
			},
			true,
		},
		{
			discovery.Service{
				InstanceId:    "\x01\x81", // Invalid UTF-8.
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123456789012345678901234567890123", // Too long.
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/y"},
			},
			false,
		},
		{
			discovery.Service{ // No interface name.
				InstanceId: "i1",
				Addrs:      []string{"/h:123/z"},
			},
			false,
		},
		{
			discovery.Service{ // No addresses.
				InstanceId:    "i1",
				InterfaceName: "v.io/v23/a",
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attrs: discovery.Attributes{
					"_key": "v", // Invalid key.
				},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attrs: discovery.Attributes{
					"k=ey": "v", // Invalid key.
				},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attachments: discovery.Attachments{
					"key\n": nil, // Invalid key.
				},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attrs: discovery.Attributes{
					"k": "\xd8\x00", // Invalid UTF-8.
				},
			},
			false,
		},
		{
			discovery.Service{
				InstanceId:    "123",
				InterfaceName: "v.io/v23/a",
				Addrs:         []string{"/h:123/x"},
				Attrs: discovery.Attributes{
					"k": "\x12\x34\xab\xcd", // Invalid UTF-8.
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
