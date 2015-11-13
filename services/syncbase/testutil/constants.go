// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

var invalidIdentifiers []string = []string{
	"/",
	"a/b",
	":",
	"a:b",
	"*",
	"\x00",
	"\x01",
	"\xfa",
	"\xfb",
	"@@",
	"dev.v.io/a/admin@myapp.com",
	"dev.v.io:a:admin@myapp.com",
	"안녕하세요",
	// 16 4-byte runes => 64 bytes
	"𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎",
}

var validIdentifiers []string = []string{
	"a",
	"B",
	"a_",
	"a__",
	"a0_",
	"a_b",
	"a_0",
	"foobar",
	"BARBAZ",
	// 64 bytes
	"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcd",
}

var universallyInvalidNames []string = []string{
	"",
	"\xfc",
	"\xfd",
	"\xfe",
	"\xff",
	"a\xfcb",
	"a\xfdb",
	"a\xfeb",
	"a\xffb",
}

var longNames []string = []string{
	// 65 bytes
	"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcde",
	// 16 4-byte runes + 1 more byte => 65 bytes
	"𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎𠜎a",
}

var OkAppNames []string = append(validIdentifiers, invalidIdentifiers...)
var OkRowNames []string = append(OkAppNames, longNames...)

var NotOkRowNames []string = universallyInvalidNames
var NotOkAppNames []string = append(NotOkRowNames, longNames...)

var OkDbTableNames []string = validIdentifiers
var NotOkDbTableNames []string = append(NotOkAppNames, invalidIdentifiers...)
