// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"errors"
	"strings"

	"v.io/v23/discovery"
)

// validateAttributes returns an error if the attributes are not suitable for advertising.
func validateAttributes(attrs discovery.Attributes) error {
	for k, _ := range attrs {
		if len(k) == 0 {
			return errors.New("empty key")
		}
		if strings.HasPrefix(k, "_") {
			return errors.New("key starts with '_'")
		}
		for _, c := range k {
			if c < 0x20 || c > 0x7e {
				return errors.New("key is not printable US-ASCII")
			}
			if c == '=' {
				return errors.New("key includes '='")
			}
		}
	}
	return nil
}

