// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"errors"
	"strings"
	"unicode/utf8"

	"v.io/v23/discovery"
)

// validateService returns an error if the service is not suitable for advertising.
func validateService(service *discovery.Service) error {
	// TODO(jhahn): Any other restriction on names in mount table?
	if !utf8.ValidString(service.InstanceId) {
		return errors.New("instance id not valid UTF-8 string")
	}
	if strings.Contains(service.InstanceId, "/") {
		return errors.New("instance id include '/'")
	}
	if len(service.InterfaceName) > 0 {
		return errors.New("interface name not supported")
	}
	if len(service.Addrs) == 0 {
		return errors.New("address not provided")
	}
	if len(service.Attrs) > 0 {
		return errors.New("attributes name not supported")
	}
	if len(service.Attachments) > 0 {
		return errors.New("attachments name not supported")
	}
	return nil
}
