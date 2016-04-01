// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"v.io/v23/discovery"
)

const (
	maxAdvertisementSize = 512
	maxAttachmentSize    = 4096
	maxNumAttributes     = 32
	maxNumAttachments    = 32
)

// validateKey returns an error if the key is not suitable for advertising.
func validateKey(key string) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	if strings.HasPrefix(key, "_") {
		return errors.New("key starts with '_'")
	}
	for _, c := range key {
		if c < 0x20 || c > 0x7e {
			return errors.New("key is not printable US-ASCII")
		}
		if c == '=' {
			return errors.New("key includes '='")
		}
	}
	return nil
}

// validateAttributes returns an error if the attributes are not suitable for advertising.
func validateAttributes(attributes discovery.Attributes) error {
	if len(attributes) > maxNumAttributes {
		return errors.New("too many")
	}
	for k, v := range attributes {
		if err := validateKey(k); err != nil {
			return err
		}
		if !utf8.ValidString(v) {
			return errors.New("value is not valid UTF-8 string")
		}
	}
	return nil
}

// validateAttachments returns an error if the attachments are not suitable for advertising.
func validateAttachments(attachments discovery.Attachments) error {
	if len(attachments) > maxNumAttachments {
		return errors.New("too many")
	}
	for k, v := range attachments {
		if err := validateKey(k); err != nil {
			return err
		}
		if len(v) > maxAttachmentSize {
			return errors.New("too large")
		}
	}
	return nil
}

// sizeOfAd returns the size of ad excluding id and attachments.
func sizeOfAd(ad *discovery.Advertisement) int {
	size := len(ad.InterfaceName)
	for _, a := range ad.Addresses {
		size += len(a)
	}
	for k, v := range ad.Attributes {
		size += len(k) + len(v)
	}
	return size
}

// validateAd returns an error if ad is not suitable for advertising.
func validateAd(ad *discovery.Advertisement) error {
	if !ad.Id.IsValid() {
		return errors.New("id not valid")
	}
	if len(ad.InterfaceName) == 0 {
		return errors.New("interface name not provided")
	}
	if len(ad.Addresses) == 0 {
		return errors.New("address not provided")
	}
	if err := validateAttributes(ad.Attributes); err != nil {
		return fmt.Errorf("attributes not valid: %v", err)
	}
	if err := validateAttachments(ad.Attachments); err != nil {
		return fmt.Errorf("attachments not valid: %v", err)
	}
	if sizeOfAd(ad) > maxAdvertisementSize {
		return errors.New("advertisement too large")
	}
	return nil
}
