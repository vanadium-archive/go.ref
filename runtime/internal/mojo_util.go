// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package internal

import (
	"flag"
	"log"
	"strings"

	"v.io/x/ref/lib/flags"
)

// NOTE(nlacasse): This variable must be set at build time by passing the
// "-ldflags" flag to "go build" like so:
// go build -ldflags "-X v.io/x/ref/runtime/internal.commandLineFlags '--flag1=foo --flag2=bar'"
var commandLineFlags string

// TODO(sadovsky): Terrible, terrible hack.
func parseFlagsInternal(f *flags.Flags, config map[string]string) error {
	// We expect that command-line flags have not been parsed. v23_util
	// performs command-line parsing at this point. For Mojo, we instead parse
	// command-line flags from the commandLineFlags variable set at build time.
	// TODO(sadovsky): Maybe move this check to util.go, or drop it?
	if flag.CommandLine.Parsed() {
		panic("flag.CommandLine.Parse() has been called")
	}

	// NOTE(nlacasse): Don't use vlog here, since vlog output depends on
	// command line flags which have not been parsed yet.
	log.Printf("Parsing flags: %v\n", commandLineFlags)

	// TODO(sadovsky): Support argument quoting. More generally, parse this env
	// var similar to how bash parses arguments.
	return f.Parse(strings.Split(commandLineFlags, " "), config)
}
