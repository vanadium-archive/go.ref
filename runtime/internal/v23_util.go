// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !mojo

package internal

import (
	"os"

	"v.io/x/ref/lib/flags"
)

func parseFlagsInternal(f *flags.Flags, config map[string]string) error {
	return f.Parse(os.Args[1:], config)
}
