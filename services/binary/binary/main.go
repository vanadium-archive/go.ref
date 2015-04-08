// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"

	"v.io/v23"
	"v.io/v23/context"

	_ "v.io/x/ref/profiles"
)

var gctx *context.T

func main() {
	var shutdown v23.Shutdown
	gctx, shutdown = v23.Init()
	exitCode := root().Main()
	shutdown()
	os.Exit(exitCode)
}
