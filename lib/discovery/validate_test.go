// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"strconv"
	"strings"
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
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attributes: discovery.Attributes{
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
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attachments: discovery.Attachments{
					"k": nil,
				},
			},
			true,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{}, // Invalid id.
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
			},
			false,
		},
		{
			discovery.Advertisement{ // No interface name.
				Id:        discovery.AdId{1, 2, 3},
				Addresses: []string{"/h:123/z"},
			},
			false,
		},
		{
			discovery.Advertisement{ // No addresses.
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attributes: discovery.Attributes{
					"_key": "v", // Invalid key.
				},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attributes: discovery.Attributes{
					"k=ey": "v", // Invalid key.
				},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attachments: discovery.Attachments{
					"key\n": nil, // Invalid key.
				},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attributes: discovery.Attributes{
					"k": "\xd8\x00", // Invalid UTF-8.
				},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attributes: discovery.Attributes{
					"k": "\x12\x34\xab\xcd", // Invalid UTF-8.
				},
			},
			false,
		},
		{
			discovery.Advertisement{ // Too large.
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: strings.Repeat("i", 100),
				Addresses:     []string{strings.Repeat("a", 100), strings.Repeat("b", 100)},
				Attributes: discovery.Attributes{
					"k12345":  strings.Repeat("v", 100),
					"k67890a": strings.Repeat("v", 100),
					// TODO(jhahn): Remove this after rolling back this temporary increase of size.
					"tmp": strings.Repeat("v", 1000),
				},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attributes:    genAttributes(33), // Too many.
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attachments: discovery.Attachments{
					"k1": bytes.Repeat([]byte{1}, 100),
					"k2": bytes.Repeat([]byte{1}, 4097), // Too large.
				},
			},
			false,
		},
		{
			discovery.Advertisement{
				Id:            discovery.AdId{1, 2, 3},
				InterfaceName: "v.io/v23/a",
				Addresses:     []string{"/h:123/x"},
				Attachments:   genAttachments(33), // Too many.
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

func genAttributes(n int) discovery.Attributes {
	attributes := make(discovery.Attributes, n)
	for i := 0; i < n; i++ {
		attributes[strconv.Itoa(i)] = ""
	}
	return attributes
}

func genAttachments(n int) discovery.Attachments {
	attachments := make(discovery.Attachments, n)
	for i := 0; i < n; i++ {
		attachments[strconv.Itoa(i)] = nil
	}
	return attachments
}
