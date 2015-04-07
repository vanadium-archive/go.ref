// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"

	"v.io/v23"

	"v.io/x/ref/services/wspr/wsprlib"
	"v.io/x/ref/test/modules"
)

var (
	port   *int    = flag.CommandLine.Int("port", 0, "Port to listen on.")
	identd *string = flag.CommandLine.String("identd", "", "identd server name. Must be set.")
)

const WSPRDCommand = "wsprd"

func init() {
	modules.RegisterChild(WSPRDCommand, modules.Usage(flag.CommandLine), startWSPR)
}

func startWSPR(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	l := v23.GetListenSpec(ctx)
	proxy := wsprlib.NewWSPR(ctx, *port, &l, *identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	fmt.Fprintf(stdout, "WSPR_ADDR=%s\n", addr)
	modules.WaitForEOF(stdin)
	return nil
}
