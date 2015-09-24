// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

var invalidIdentifiers []string = []string{
	"/",
	"a/b",
	"*",
	"@@",
	"dev.v.io/a/admin@myapp.com",
	"안녕하세요",
}

var OkDbTableNames []string = []string{
	"a",
	"B",
	"a_",
	"a__",
	"a0_",
	"a_b",
	"a_0",
	"foobar",
	"BARBAZ",
}

var NotOkAppRowNames []string = []string{
	"",
	":",
	"\x00",
	"\xff",
	"a:b",
	"a\x00b",
	"a\xffb",
}

var OkAppRowNames []string = append(OkDbTableNames, invalidIdentifiers...)

var NotOkDbTableNames []string = append(NotOkAppRowNames, invalidIdentifiers...)
