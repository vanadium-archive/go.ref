// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package internal

import (
	"flag"
	"os"
	"strings"

	"v.io/x/ref/lib/flags"
)

// TODO(sadovsky): Terrible, terrible hack.
func parseFlagsInternal(f *flags.Flags, config map[string]string) error {
	// We expect that command-line flags have not been parsed. v23_util performs
	// command-line parsing at this point. For Mojo, we instead parse command-line
	// flags from the V23_MOJO_FLAGS env var.
	// TODO(sadovsky): Maybe move this check to util.go, or drop it?
	if flag.CommandLine.Parsed() {
		panic("flag.CommandLine.Parse() has been called")
	}
	// TODO(sadovsky): Support argument quoting. More generally, parse this env
	// var similar to how bash parses arguments.
	return f.Parse(strings.Split(os.Getenv("V23_MOJO_FLAGS"), " "), config)
}
