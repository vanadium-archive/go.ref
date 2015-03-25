// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "unicode"

func LowercaseFirstCharacter(s string) string {
	for _, r := range s {
		return string(unicode.ToLower(r)) + s[1:]
	}
	return ""
}

func UppercaseFirstCharacter(s string) string {
	for _, r := range s {
		return string(unicode.ToUpper(r)) + s[1:]
	}
	return ""
}
