// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"regexp"
	"strings"
)

// TODO(sadovsky): Consider loosening. Perhaps model after MySQL:
// http://dev.mysql.com/doc/refman/5.7/en/identifiers.html
var keyAtomRegexp *regexp.Regexp = regexp.MustCompile("^[a-zA-Z0-9_.-]+$")

func validKeyAtom(s string) bool {
	return keyAtomRegexp.MatchString(s)
}

func joinKeyParts(parts ...string) string {
	return strings.Join(parts, ":")
}
