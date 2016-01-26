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
	// Limit the maximum length of instance id. Some plugins are using an instance id
	// as a key (e.g., as a hostname in mDNS) and so its size can be limited by their
	// protocols.
	maxInstanceIdLen = 32
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
func validateAttributes(attrs discovery.Attributes) error {
	for k, v := range attrs {
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
	for k, _ := range attachments {
		if err := validateKey(k); err != nil {
			return err
		}
	}
	return nil
}

// validateService returns an error if the service is not suitable for advertising.
func validateService(service *discovery.Service) error {
	if len(service.InstanceId) > maxInstanceIdLen {
		return errors.New("instance id too long")
	}
	if !utf8.ValidString(service.InstanceId) {
		return errors.New("instance id not valid UTF-8 string")
	}
	if len(service.InterfaceName) == 0 {
		return errors.New("interface name not provided")
	}
	if len(service.Addrs) == 0 {
		return errors.New("address not provided")
	}
	if err := validateAttributes(service.Attrs); err != nil {
		return fmt.Errorf("attributes not valid: %v", err)
	}
	if err := validateAttachments(service.Attachments); err != nil {
		return fmt.Errorf("attachments not valid: %v", err)
	}
	return nil
}
