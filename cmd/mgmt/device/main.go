// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"

	"v.io/v23"

	"v.io/x/ref/cmd/mgmt/device/impl"
	_ "v.io/x/ref/profiles/static"
)

func main() {
	gctx, shutdown := v23.Init()
	impl.SetGlobalContext(gctx)
	exitCode := impl.Root().Main()
	shutdown()
	os.Exit(exitCode)
}
